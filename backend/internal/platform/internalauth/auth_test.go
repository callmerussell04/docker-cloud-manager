package internalauth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	gatewayCredential = "gateway-credential"
	builderCredential = "builder-credential"

	openMethod    = "/core.ContainerAPI/ListContainers"
	builderMethod = "/core.ImageAPI/StartBuildRecord"
)

func testAuthenticator(t *testing.T) *internalauth.Authenticator {
	t.Helper()
	auth, err := internalauth.NewAuthenticator(
		internalauth.CallerPolicy{
			Caller:     internalauth.CallerGateway,
			Credential: gatewayCredential,
			Methods:    internalauth.AllMethodsExcept(builderMethod),
			Scopes:     []accessscope.Kind{accessscope.KindUser, accessscope.KindAdmin, accessscope.KindSystem},
			HTTPRoutes: true,
		},
		internalauth.CallerPolicy{
			Caller:     internalauth.CallerBuilder,
			Credential: builderCredential,
			Methods:    internalauth.OnlyMethods(builderMethod),
			Scopes:     []accessscope.Kind{accessscope.KindSystem},
		},
	)
	require.NoError(t, err)
	return auth
}

// TestAuthenticatorEnforcesCallerPolicy drives one table through both the gRPC
// interceptor and the HTTP middleware. A refused request never reaches the
// handler.
func TestAuthenticatorEnforcesCallerPolicy(t *testing.T) {
	userID := uuid.New()
	userScope := &accessscope.Scope{Kind: accessscope.KindUser, UserID: userID, Username: "alice", Role: "user"}
	adminScope := &accessscope.Scope{Kind: accessscope.KindAdmin, UserID: userID, Username: "root", Role: "admin"}
	systemScope := &accessscope.Scope{Kind: accessscope.KindSystem}

	for _, tc := range []struct {
		name       string
		credential string
		method     string
		scope      *accessscope.Scope
		wantGRPC   codes.Code
		// wantHTTP differs where HTTP has no method rule (any internal route
		// is one call) or the caller has no HTTP routes at all.
		wantHTTP int
	}{
		{name: "gateway states a user scope", credential: gatewayCredential, method: openMethod, scope: userScope, wantGRPC: codes.OK, wantHTTP: http.StatusNoContent},
		{name: "gateway states an admin scope", credential: gatewayCredential, method: openMethod, scope: adminScope, wantGRPC: codes.OK, wantHTTP: http.StatusNoContent},
		{name: "gateway states a system scope", credential: gatewayCredential, method: openMethod, scope: systemScope, wantGRPC: codes.OK, wantHTTP: http.StatusNoContent},
		{name: "builder makes its own call", credential: builderCredential, method: builderMethod, scope: systemScope, wantGRPC: codes.OK, wantHTTP: http.StatusForbidden},
		{name: "unknown credential", credential: "someone-else", method: openMethod, scope: systemScope, wantGRPC: codes.Unauthenticated, wantHTTP: http.StatusUnauthorized},
		{name: "credential prefix", credential: gatewayCredential[:4], method: openMethod, scope: systemScope, wantGRPC: codes.Unauthenticated, wantHTTP: http.StatusUnauthorized},
		{name: "no credential", credential: "", method: openMethod, scope: systemScope, wantGRPC: codes.Unauthenticated, wantHTTP: http.StatusUnauthorized},
		{name: "builder outside its policy", credential: builderCredential, method: openMethod, scope: systemScope, wantGRPC: codes.PermissionDenied, wantHTTP: http.StatusForbidden},
		{name: "gateway on an excluded call", credential: gatewayCredential, method: builderMethod, scope: systemScope, wantGRPC: codes.PermissionDenied, wantHTTP: http.StatusNoContent},
		{name: "builder states a user scope", credential: builderCredential, method: builderMethod, scope: userScope, wantGRPC: codes.PermissionDenied, wantHTTP: http.StatusForbidden},
		{name: "builder states an admin scope", credential: builderCredential, method: builderMethod, scope: adminScope, wantGRPC: codes.PermissionDenied, wantHTTP: http.StatusForbidden},
		{name: "missing scope", credential: gatewayCredential, method: openMethod, scope: nil, wantGRPC: codes.Unauthenticated, wantHTTP: http.StatusUnauthorized},
		{name: "unknown scope kind", credential: gatewayCredential, method: openMethod, scope: &accessscope.Scope{Kind: "root"}, wantGRPC: codes.Unauthenticated, wantHTTP: http.StatusUnauthorized},
	} {
		t.Run(tc.name+" over gRPC", func(t *testing.T) {
			pairs := []string{}
			if tc.credential != "" {
				pairs = append(pairs, internalauth.MetadataKey, tc.credential)
			}
			pairs = append(pairs, scopeMetadata(tc.scope)...)

			var got accessscope.Scope
			called := false
			_, err := testAuthenticator(t).UnaryServerInterceptor()(
				metadata.NewIncomingContext(context.Background(), metadata.Pairs(pairs...)), nil,
				&grpc.UnaryServerInfo{FullMethod: tc.method},
				func(ctx context.Context, _ any) (any, error) {
					called = true
					got, _ = accessscope.FromContext(ctx)
					return nil, nil
				})

			require.Equal(t, tc.wantGRPC, status.Code(err))
			require.Equal(t, tc.wantGRPC == codes.OK, called, "only an allowed call reaches the handler")
			if called {
				require.Equal(t, *tc.scope, got)
			}
			if err != nil && tc.credential != "" {
				require.NotContains(t, err.Error(), tc.credential, "a refusal never echoes the credential")
			}
		})
		t.Run(tc.name+" over HTTP", func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			var got accessscope.Scope
			called := false
			router.GET("/internal", testAuthenticator(t).Middleware(), func(c *gin.Context) {
				called = true
				got, _ = accessscope.FromContext(c.Request.Context())
				c.Status(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, "/internal", nil)
			if tc.credential != "" {
				req.Header.Set(internalauth.HeaderName, tc.credential)
			}
			setScopeHeaders(req.Header, tc.scope)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, tc.wantHTTP, rec.Code)
			require.Equal(t, tc.wantHTTP == http.StatusNoContent, called, "only an allowed request reaches the handler")
			if called {
				require.Equal(t, *tc.scope, got)
			}
			if tc.credential != "" {
				require.NotContains(t, rec.Body.String(), tc.credential)
			}
		})
	}
}

func TestNewAuthenticatorRejectsUnusableCredentials(t *testing.T) {
	policy := func(caller internalauth.Caller, credential string) internalauth.CallerPolicy {
		return internalauth.CallerPolicy{
			Caller: caller, Credential: credential,
			Methods: internalauth.AllMethods(), Scopes: []accessscope.Kind{accessscope.KindSystem},
		}
	}
	for _, tc := range []struct {
		name     string
		policies []internalauth.CallerPolicy
	}{
		{name: "no callers"},
		{name: "empty credential", policies: []internalauth.CallerPolicy{policy(internalauth.CallerGateway, "")}},
		{name: "shared credential", policies: []internalauth.CallerPolicy{policy(internalauth.CallerGateway, "same-secret"), policy(internalauth.CallerBuilder, "same-secret")}},
		{name: "repeated caller", policies: []internalauth.CallerPolicy{policy(internalauth.CallerGateway, "one-secret"), policy(internalauth.CallerGateway, "two-secret")}},
		{name: "no scope kinds", policies: []internalauth.CallerPolicy{{Caller: internalauth.CallerGateway, Credential: "a-secret", Methods: internalauth.AllMethods()}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := internalauth.NewAuthenticator(tc.policies...)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret", "a construction error never contains a credential")
		})
	}
}

func TestZeroMethodRuleAllowsNoCall(t *testing.T) {
	auth, err := internalauth.NewAuthenticator(internalauth.CallerPolicy{
		Caller: internalauth.CallerBuilder, Credential: builderCredential, Scopes: []accessscope.Kind{accessscope.KindSystem},
	})
	require.NoError(t, err)
	md := metadata.Pairs(append([]string{internalauth.MetadataKey, builderCredential}, scopeMetadata(&accessscope.Scope{Kind: accessscope.KindSystem})...)...)
	_, err = auth.UnaryServerInterceptor()(metadata.NewIncomingContext(context.Background(), md), nil,
		&grpc.UnaryServerInfo{FullMethod: builderMethod},
		func(context.Context, any) (any, error) { return nil, nil })
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestClientInterceptorsSendCredentialAndScope(t *testing.T) {
	actorID := uuid.New()
	adminCtx := accessscope.WithAdminScope(context.Background(), actorID, "root", "admin")

	for _, tc := range []struct {
		name        string
		interceptor grpc.UnaryClientInterceptor
		credential  string
		method      string
		want        accessscope.Scope
	}{
		{
			name: "forwards the context scope", interceptor: internalauth.UnaryClientInterceptor(gatewayCredential),
			credential: gatewayCredential, method: openMethod,
			want: accessscope.Scope{Kind: accessscope.KindAdmin, UserID: actorID, Username: "root", Role: "admin"},
		},
		{
			name: "always states system scope", interceptor: internalauth.SystemScopeUnaryClientInterceptor(builderCredential),
			credential: builderCredential, method: builderMethod,
			want: accessscope.Scope{Kind: accessscope.KindSystem},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.interceptor(adminCtx, tc.method, nil, nil, nil, func(outgoing context.Context, method string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
				md, ok := metadata.FromOutgoingContext(outgoing)
				require.True(t, ok, "outgoing metadata was not set")
				require.Equal(t, []string{tc.credential}, md.Get(internalauth.MetadataKey))
				_, err := testAuthenticator(t).UnaryServerInterceptor()(metadata.NewIncomingContext(context.Background(), md), nil,
					&grpc.UnaryServerInfo{FullMethod: method},
					func(incoming context.Context, _ any) (any, error) {
						scope, ok := accessscope.FromContext(incoming)
						require.True(t, ok)
						require.Equal(t, tc.want, scope)
						return nil, nil
					})
				return err
			})
			require.NoError(t, err)
		})
	}
}

func TestSetScopeHeadersFromContext(t *testing.T) {
	actorID := uuid.New()
	header := make(http.Header)

	internalauth.SetScopeHeadersFromContext(header, accessscope.WithUserScope(context.Background(), actorID, "alice", "user"))

	require.Equal(t, string(accessscope.KindUser), header.Get(internalauth.HeaderScope))
	require.Equal(t, actorID.String(), header.Get(internalauth.HeaderUserID))
	require.Equal(t, "alice", header.Get(internalauth.HeaderUsername))
	require.Equal(t, "user", header.Get(internalauth.HeaderRole))
}

func TestInternalAuthRejectsUserScopeWithoutUserID(t *testing.T) {
	for _, tc := range []struct {
		name   string
		userID string
	}{
		{name: "all-zero UUID", userID: uuid.Nil.String()},
		{name: "missing user ID", userID: ""},
		{name: "malformed user ID", userID: "not-a-uuid"},
	} {
		t.Run(tc.name+" over gRPC metadata", func(t *testing.T) {
			md := metadata.Pairs(
				internalauth.MetadataKey, gatewayCredential,
				internalauth.MetadataScope, string(accessscope.KindUser),
				internalauth.MetadataUserID, tc.userID,
			)
			called := false
			_, err := testAuthenticator(t).UnaryServerInterceptor()(
				metadata.NewIncomingContext(context.Background(), md), nil,
				&grpc.UnaryServerInfo{FullMethod: openMethod},
				func(context.Context, any) (any, error) {
					called = true
					return nil, nil
				})
			require.Equal(t, codes.Unauthenticated, status.Code(err))
			require.False(t, called, "handler must not run without a valid user ID")
		})
		t.Run(tc.name+" over HTTP headers", func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			called := false
			router.GET("/internal", testAuthenticator(t).Middleware(), func(c *gin.Context) {
				called = true
				c.Status(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, "/internal", nil)
			req.Header.Set(internalauth.HeaderName, gatewayCredential)
			req.Header.Set(internalauth.HeaderScope, string(accessscope.KindUser))
			req.Header.Set(internalauth.HeaderUserID, tc.userID)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusUnauthorized, rec.Code)
			require.False(t, called, "handler must not run without a valid user ID")
		})
	}
}

func scopeMetadata(scope *accessscope.Scope) []string {
	if scope == nil {
		return nil
	}
	pairs := []string{internalauth.MetadataScope, string(scope.Kind)}
	if scope.UserID != uuid.Nil {
		pairs = append(pairs, internalauth.MetadataUserID, scope.UserID.String())
	}
	if scope.Username != "" {
		pairs = append(pairs, internalauth.MetadataUsername, scope.Username)
	}
	if scope.Role != "" {
		pairs = append(pairs, internalauth.MetadataRole, scope.Role)
	}
	return pairs
}

func setScopeHeaders(header http.Header, scope *accessscope.Scope) {
	// Header.Set canonicalizes the lower-case metadata keys into the
	// matching header names.
	pairs := scopeMetadata(scope)
	for i := 0; i < len(pairs); i += 2 {
		header.Set(pairs[i], pairs[i+1])
	}
}

// TestRequestLogsNameTheCallerWithoutTheCredential runs the authenticator
// inside the request log interceptor and middleware, as the servers chain them.
func TestRequestLogsNameTheCallerWithoutTheCredential(t *testing.T) {
	scope := &accessscope.Scope{Kind: accessscope.KindSystem}

	t.Run("gRPC", func(t *testing.T) {
		var logs bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logs, nil))
		auth := testAuthenticator(t).UnaryServerInterceptor()
		md := metadata.Pairs(append([]string{internalauth.MetadataKey, builderCredential}, scopeMetadata(scope)...)...)
		_, err := logging.UnaryServerInterceptor(logger)(metadata.NewIncomingContext(context.Background(), md), nil,
			&grpc.UnaryServerInfo{FullMethod: openMethod},
			func(ctx context.Context, req any) (any, error) {
				return auth(ctx, req, &grpc.UnaryServerInfo{FullMethod: openMethod}, func(context.Context, any) (any, error) { return nil, nil })
			})
		require.Equal(t, codes.PermissionDenied, status.Code(err))
		require.Equal(t, "builder", logEntry(t, &logs)[logging.CallerKey], "a refused call still names its caller")
		require.NotContains(t, logs.String(), builderCredential)
	})
	t.Run("HTTP", func(t *testing.T) {
		var logs bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logs, nil))
		gin.SetMode(gin.TestMode)
		router := gin.New()
		router.Use(logging.AccessLogMiddleware(logger))
		router.GET("/internal", testAuthenticator(t).Middleware(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
		req := httptest.NewRequest(http.MethodGet, "/internal", nil)
		req.Header.Set(internalauth.HeaderName, gatewayCredential)
		setScopeHeaders(req.Header, scope)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusNoContent, rec.Code)
		require.Equal(t, "gateway", logEntry(t, &logs)[logging.CallerKey])
		require.NotContains(t, logs.String(), gatewayCredential)
	})
}

func logEntry(t *testing.T, logs *bytes.Buffer) map[string]any {
	t.Helper()
	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))
	return entry
}
