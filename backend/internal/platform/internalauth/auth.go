// Package internalauth authenticates calls between DCM services. A server
// identifies the calling service by its credential, applies that caller's
// policy (which calls it may make and which actor scope kinds it may state),
// and restores the stated scope into the request context.
package internalauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/httpresponse"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	HeaderName  = "X-Internal-Token"
	MetadataKey = "x-internal-token"

	HeaderScope    = "X-Actor-Scope"
	HeaderUserID   = "X-User-Id"
	HeaderUsername = "X-Username"
	HeaderRole     = "X-User-Role"

	MetadataScope    = "x-actor-scope"
	MetadataUserID   = "x-user-id"
	MetadataUsername = "x-username"
	MetadataRole     = "x-user-role"
)

// Caller names a DCM service that calls another one.
type Caller string

const (
	CallerGateway Caller = "gateway"
	CallerCore    Caller = "core"
	CallerBuilder Caller = "builder"
)

// MethodRule is the set of gRPC methods a caller may call. Its zero value
// allows none.
type MethodRule struct {
	all    bool
	listed map[string]struct{}
}

// AllMethods allows every method.
func AllMethods() MethodRule {
	return MethodRule{all: true}
}

// AllMethodsExcept allows every method but the full method names given.
func AllMethodsExcept(fullMethods ...string) MethodRule {
	return MethodRule{all: true, listed: methodSet(fullMethods)}
}

// OnlyMethods allows only the full method names given.
func OnlyMethods(fullMethods ...string) MethodRule {
	return MethodRule{listed: methodSet(fullMethods)}
}

func (r MethodRule) allows(fullMethod string) bool {
	_, listed := r.listed[fullMethod]
	return r.all != listed
}

func methodSet(fullMethods []string) map[string]struct{} {
	set := make(map[string]struct{}, len(fullMethods))
	for _, method := range fullMethods {
		set[method] = struct{}{}
	}
	return set
}

// CallerPolicy is what one calling service may do on a server.
type CallerPolicy struct {
	Caller     Caller
	Credential string
	// Methods applies to gRPC calls.
	Methods MethodRule
	// Scopes are the actor scope kinds the caller may state.
	Scopes []accessscope.Kind
	// HTTPRoutes allows the server's internal HTTP routes.
	HTTPRoutes bool
}

type callerEntry struct {
	policy CallerPolicy
	digest [sha256.Size]byte
}

// Authenticator identifies callers and enforces their policies on a server.
type Authenticator struct {
	callers []callerEntry
}

// NewAuthenticator checks the policy table. A missing or empty credential, a
// credential two callers share, a repeated caller or a caller allowed no scope
// kind is an error that names callers, never a credential.
func NewAuthenticator(policies ...CallerPolicy) (*Authenticator, error) {
	if len(policies) == 0 {
		return nil, errors.New("internal auth needs at least one caller")
	}
	callers := make([]callerEntry, 0, len(policies))
	for _, policy := range policies {
		if policy.Caller == "" {
			return nil, errors.New("internal auth caller has no name")
		}
		if policy.Credential == "" {
			return nil, fmt.Errorf("internal auth credential for caller %s is empty", policy.Caller)
		}
		if len(policy.Scopes) == 0 {
			return nil, fmt.Errorf("internal auth caller %s may state no scope kind", policy.Caller)
		}
		entry := callerEntry{policy: policy, digest: sha256.Sum256([]byte(policy.Credential))}
		for _, other := range callers {
			if other.policy.Caller == policy.Caller {
				return nil, fmt.Errorf("internal auth caller %s is listed twice", policy.Caller)
			}
			if other.digest == entry.digest {
				return nil, fmt.Errorf("internal auth callers %s and %s share a credential", other.policy.Caller, policy.Caller)
			}
		}
		callers = append(callers, entry)
	}
	return &Authenticator{callers: callers}, nil
}

// identify compares the presented credential with every caller's in constant
// time. Comparing digests keeps the comparison independent of length.
func (a *Authenticator) identify(presented string) (CallerPolicy, bool) {
	if presented == "" {
		return CallerPolicy{}, false
	}
	digest := sha256.Sum256([]byte(presented))
	found := -1
	for i, caller := range a.callers {
		if subtle.ConstantTimeCompare(digest[:], caller.digest[:]) == 1 {
			found = i
		}
	}
	if found < 0 {
		return CallerPolicy{}, false
	}
	return a.callers[found].policy, true
}

var (
	errUnknownCaller  = fmt.Errorf("%w: invalid internal credential", apperrors.ErrUnauthorized)
	errCallNotAllowed = fmt.Errorf("%w: call is not allowed for this caller", apperrors.ErrForbidden)
	errScopeRequired  = fmt.Errorf("%w: actor scope is required", apperrors.ErrUnauthorized)
	errScopeInvalid   = fmt.Errorf("%w: invalid actor scope", apperrors.ErrUnauthorized)
	errScopeForbidden = fmt.Errorf("%w: actor scope is not allowed for this caller", apperrors.ErrForbidden)
)

type scopeValues struct {
	kind, userID, username, role string
}

// authorize restores the stated actor scope into ctx once the caller may
// state its kind.
func authorize(ctx context.Context, policy CallerPolicy, values scopeValues) (context.Context, error) {
	if values.kind == "" {
		return nil, errScopeRequired
	}
	scopedCtx, err := scopeContext(ctx, values)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(policy.Scopes, accessscope.Kind(values.kind)) {
		return nil, errScopeForbidden
	}
	return scopedCtx, nil
}

// UnaryServerInterceptor authenticates gRPC calls. An unknown credential, a
// missing or malformed scope is Unauthenticated; a call or scope kind outside
// the caller's policy is PermissionDenied. Neither reaches the handler.
func (a *Authenticator) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		policy, ok := a.identify(firstMetadata(md, MetadataKey))
		if !ok {
			return nil, grpcError(errUnknownCaller)
		}
		logging.SetCaller(ctx, string(policy.Caller))
		if !policy.Methods.allows(info.FullMethod) {
			return nil, grpcError(errCallNotAllowed)
		}
		ctx, err := authorize(ctx, policy, scopeValues{
			kind:     firstMetadata(md, MetadataScope),
			userID:   firstMetadata(md, MetadataUserID),
			username: firstMetadata(md, MetadataUsername),
			role:     firstMetadata(md, MetadataRole),
		})
		if err != nil {
			return nil, grpcError(err)
		}
		return handler(ctx, req)
	}
}

// Middleware authenticates internal HTTP requests with the same rules; a
// caller whose policy has no HTTP routes is forbidden.
func (a *Authenticator) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		policy, ok := a.identify(c.GetHeader(HeaderName))
		if !ok {
			httpresponse.Respond(c, http.StatusUnauthorized, errUnknownCaller)
			return
		}
		logging.SetCaller(c.Request.Context(), string(policy.Caller))
		if !policy.HTTPRoutes {
			httpresponse.Respond(c, http.StatusForbidden, errCallNotAllowed)
			return
		}
		header := c.Request.Header
		ctx, err := authorize(c.Request.Context(), policy, scopeValues{
			kind:     header.Get(HeaderScope),
			userID:   header.Get(HeaderUserID),
			username: header.Get(HeaderUsername),
			role:     header.Get(HeaderRole),
		})
		if err != nil {
			httpresponse.Respond(c, httpresponse.Status(err), err)
			return
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func grpcError(err error) error {
	code := codes.Unauthenticated
	if errors.Is(err, apperrors.ErrForbidden) {
		code = codes.PermissionDenied
	}
	return status.Error(code, apperrors.SafeMessage(err))
}

// UnaryClientInterceptor sends the caller's credential and the actor scope
// held by the call's context. A context without a scope sends none, and the
// server refuses the call.
func UnaryClientInterceptor(credential string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, MetadataKey, credential)
		ctx = appendScopeMetadata(ctx)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// SystemScopeUnaryClientInterceptor sends the caller's credential and always
// states system scope, whatever scope the call's context holds. Callers
// allowed only system scope use it.
func SystemScopeUnaryClientInterceptor(credential string) grpc.UnaryClientInterceptor {
	forward := UnaryClientInterceptor(credential)
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return forward(accessscope.WithSystemScope(ctx), method, req, reply, cc, invoker, opts...)
	}
}

func SetScopeHeadersFromContext(header http.Header, ctx context.Context) {
	scope, ok := accessscope.FromContext(ctx)
	if !ok {
		return
	}
	header.Set(HeaderScope, string(scope.Kind))
	if scope.UserID != uuid.Nil {
		header.Set(HeaderUserID, scope.UserID.String())
	}
	if scope.Username != "" {
		header.Set(HeaderUsername, scope.Username)
	}
	if scope.Role != "" {
		header.Set(HeaderRole, scope.Role)
	}
}

func appendScopeMetadata(ctx context.Context) context.Context {
	scope, ok := accessscope.FromContext(ctx)
	if !ok {
		return ctx
	}
	pairs := []string{MetadataScope, string(scope.Kind)}
	if scope.UserID != uuid.Nil {
		pairs = append(pairs, MetadataUserID, scope.UserID.String())
	}
	if scope.Username != "" {
		pairs = append(pairs, MetadataUsername, scope.Username)
	}
	if scope.Role != "" {
		pairs = append(pairs, MetadataRole, scope.Role)
	}
	return metadata.AppendToOutgoingContext(ctx, pairs...)
}

func scopeContext(ctx context.Context, values scopeValues) (context.Context, error) {
	userIDValue, username, role := values.userID, values.username, values.role
	switch accessscope.Kind(values.kind) {
	case accessscope.KindUser:
		// The all-zero UUID would name no owner, so a user scope must carry a real one.
		userID, err := uuid.Parse(userIDValue)
		if err != nil || userID == uuid.Nil {
			return nil, errScopeInvalid
		}
		return accessscope.WithUserScope(ctx, userID, username, role), nil
	case accessscope.KindAdmin:
		userID, err := uuid.Parse(userIDValue)
		if err != nil {
			return nil, errScopeInvalid
		}
		return accessscope.WithAdminScope(ctx, userID, username, role), nil
	case accessscope.KindSystem:
		return accessscope.WithSystemScope(ctx), nil
	default:
		return nil, errScopeInvalid
	}
}

func firstMetadata(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
