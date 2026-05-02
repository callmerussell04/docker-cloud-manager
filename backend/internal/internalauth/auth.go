package internalauth

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
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

func UnaryServerInterceptor(token string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if token == "" {
			return nil, status.Error(codes.Unauthenticated, "internal token is not configured")
		}
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok || len(md.Get(MetadataKey)) == 0 || md.Get(MetadataKey)[0] != token {
			return nil, status.Error(codes.Unauthenticated, "invalid internal token")
		}
		ctx, err := scopeContextFromMetadata(ctx, md)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid actor scope")
		}
		return handler(ctx, req)
	}
}

func UnaryClientInterceptor(token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, MetadataKey, token)
		ctx = appendScopeMetadata(ctx)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func Middleware(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token == "" || c.GetHeader(HeaderName) != token {
			httpresponse.Respond(c, http.StatusUnauthorized, apperrors.ErrUnauthorized)
			return
		}
		ctx, err := scopeContextFromHeaders(c.Request.Context(), c.Request.Header)
		if err != nil {
			httpresponse.Respond(c, httpresponse.Status(err), err)
			return
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
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

func scopeContextFromMetadata(ctx context.Context, md metadata.MD) (context.Context, error) {
	scopeValue := firstMetadata(md, MetadataScope)
	if scopeValue == "" {
		return accessscope.WithSystemScope(ctx), nil
	}
	return scopeContext(ctx, scopeValue, firstMetadata(md, MetadataUserID), firstMetadata(md, MetadataUsername), firstMetadata(md, MetadataRole))
}

func scopeContextFromHeaders(ctx context.Context, header http.Header) (context.Context, error) {
	scopeValue := header.Get(HeaderScope)
	if scopeValue == "" {
		return accessscope.WithSystemScope(ctx), nil
	}
	return scopeContext(ctx, scopeValue, header.Get(HeaderUserID), header.Get(HeaderUsername), header.Get(HeaderRole))
}

func scopeContext(ctx context.Context, kindValue, userIDValue, username, role string) (context.Context, error) {
	kind := accessscope.Kind(kindValue)
	switch kind {
	case accessscope.KindUser:
		userID, err := uuid.Parse(userIDValue)
		if err != nil {
			return nil, apperrors.ErrUnauthorized
		}
		return accessscope.WithUserScope(ctx, userID, username, role), nil
	case accessscope.KindAdmin:
		userID, err := uuid.Parse(userIDValue)
		if err != nil {
			return nil, apperrors.ErrUnauthorized
		}
		return accessscope.WithAdminScope(ctx, userID, username, role), nil
	case accessscope.KindSystem:
		return accessscope.WithSystemScope(ctx), nil
	default:
		return nil, apperrors.ErrUnauthorized
	}
}

func firstMetadata(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
