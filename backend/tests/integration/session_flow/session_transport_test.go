package session_flow_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/api/sso"
	coreclient "github.com/callmerussell04/docker-cloud-manager/backend/internal/core/grpc/client"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/grpc/client"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/internalauth"
	ssogrpc "github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/grpc"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/jwt"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const (
	gatewayCredential = "gateway-test-credential"
	coreCredential    = "core-test-credential"
)

// serveSSO serves svc behind SSO's production caller policies and returns a
// dialer whose connections send what the given client interceptor adds.
func serveSSO(t *testing.T, svc *service.AuthService) func(grpc.UnaryClientInterceptor) *grpc.ClientConn {
	t.Helper()
	callers, err := internalauth.NewAuthenticator(ssogrpc.CallerPolicies(ssogrpc.CallerCredentials{
		Gateway: gatewayCredential, Core: coreCredential,
	})...)
	require.NoError(t, err)
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.UnaryInterceptor(callers.UnaryServerInterceptor()))
	ssogrpc.Register(server, svc)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return func(interceptor grpc.UnaryClientInterceptor) *grpc.ClientConn {
		conn, err := grpc.NewClient("passthrough:///sso", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithUnaryInterceptor(interceptor))
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
}

// Core's credential reads users through Core's own SSO client and is refused
// every other SSO call, whatever scope it states.
func TestCoreCredentialOnlyReadsUsersFromSSO(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db := dbtest.OpenSSOPostgres(t)
	svc := service.NewAuthServiceWithConfig(repository.NewUserRepository(db), jwt.NewProvider("secret"), repository.NewSessionRepository(db), nil, service.Config{
		LocalLoginEnabled: true, LocalRegisterEnabled: true,
	})
	aliceID, err := svc.Register(ctx, "alice", "alice@example.com", "secret123")
	require.NoError(t, err)
	tokens, err := svc.Login(ctx, "alice", "secret123")
	require.NoError(t, err)
	dial := serveSSO(t, svc)

	// Core's production client wiring: its own credential, system scope only.
	coreConn := dial(internalauth.SystemScopeUnaryClientInterceptor(coreCredential))
	users := coreclient.NewSSOClient(coreConn)
	// Core may be serving a user's request; it still states system scope.
	userCtx := accessscope.WithUserScope(ctx, aliceID, "alice", "user")
	user, err := users.GetUser(userCtx, aliceID)
	require.NoError(t, err)
	require.Equal(t, "alice", user.Username)
	byID, err := users.GetUsers(userCtx, []uuid.UUID{aliceID})
	require.NoError(t, err)
	require.Equal(t, "alice", byID[aliceID].Username)

	authAPI := sso.NewAuthClient(coreConn)
	userAPI := sso.NewUserAPIClient(coreConn)
	for name, call := range map[string]func() error{
		"login": func() error {
			_, err := authAPI.Login(ctx, &sso.LoginRequest{Username: "alice", Password: "secret123"})
			return err
		},
		"list users": func() error {
			_, err := userAPI.ListUsers(ctx, &sso.ListUsersRequest{})
			return err
		},
		"create user": func() error {
			_, err := userAPI.CreateUser(ctx, &sso.CreateUserRequest{Username: "mallory", Email: "m@example.com", Password: "secret123", Role: "admin"})
			return err
		},
		"deactivate user": func() error {
			_, err := userAPI.DeactivateUser(ctx, &sso.UserIDRequest{UserId: aliceID.String()})
			return err
		},
		"verify token": func() error {
			_, err := userAPI.VerifyAccessToken(ctx, &sso.VerifyTokenRequest{AccessToken: tokens.AccessToken})
			return err
		},
	} {
		require.Equal(t, codes.PermissionDenied, status.Code(call()), name)
	}

	// Core's credential may not state a user or admin scope even on its reads.
	for _, scoped := range []context.Context{
		accessscope.WithUserScope(ctx, aliceID, "alice", "user"),
		accessscope.WithAdminScope(ctx, aliceID, "alice", "admin"),
	} {
		forwarding := sso.NewUserAPIClient(dial(internalauth.UnaryClientInterceptor(coreCredential)))
		_, err := forwarding.GetUser(scoped, &sso.GetUserRequest{UserId: aliceID.String()})
		require.Equal(t, codes.PermissionDenied, status.Code(err))
	}
	still, err := users.GetUser(ctx, aliceID)
	require.NoError(t, err)
	require.Equal(t, "active", still.Status, "no refused call reached the service")
}

// One real Gateway -> SSO RPC -> PostgreSQL path verifies the security contract
// at the public HTTP and internal RPC boundaries without mocking lifecycle policy.
func TestSessionLifecycleThroughHTTPAndRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db := dbtest.OpenSSOPostgres(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	svc := service.NewAuthServiceWithConfig(repository.NewUserRepository(db), jwt.NewProvider("secret"), repository.NewSessionRepository(db), nil, service.Config{
		LocalLoginEnabled: true, LocalRegisterEnabled: true, Now: func() time.Time { return now },
	})
	_, err := svc.Register(ctx, "alice", "alice@example.com", "secret123")
	require.NoError(t, err)
	conn := serveSSO(t, svc)(internalauth.UnaryClientInterceptor(gatewayCredential))
	ssoClient := client.NewSSOClient(conn)
	// Direct token checks state system scope, as Gateway's client does.
	rpcCtx := accessscope.WithSystemScope(ctx)
	auth := handler.NewAuthHandler(ssoClient, handler.CookieConfig{Name: "refresh_token", HTTPOnly: true, Path: "/api/v1/auth"}, handler.AuthRedirectConfig{CookieAllowedOrigins: []string{"http://frontend.example"}})
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/login", auth.Login)
	router.POST("/refresh", auth.Refresh)
	router.POST("/logout", auth.Logout)
	request := func(path, value, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
		req.Header.Set("Origin", "http://frontend.example")
		req.Header.Set("Content-Type", "application/json")
		if value != "" {
			req.AddCookie(&http.Cookie{Name: "refresh_token", Value: value})
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	credentials := func(response *httptest.ResponseRecorder) (string, string) {
		t.Helper()
		require.Equal(t, http.StatusOK, response.Code)
		require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		var body map[string]string
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		require.Len(t, body, 1)
		var refresh string
		for _, cookie := range response.Result().Cookies() {
			if cookie.Name == "refresh_token" {
				refresh = cookie.Value
				require.True(t, cookie.HttpOnly)
				require.LessOrEqual(t, cookie.MaxAge, 7*24*3600)
				require.Greater(t, cookie.MaxAge, 7*24*3600-60)
			}
		}
		require.Len(t, refresh, 43)
		return body["access_token"], refresh
	}
	login := func() (string, string) {
		return credentials(request("/login", "", `{"username":"alice","password":"secret123"}`))
	}
	require.Equal(t, http.StatusUnauthorized, request("/login", "", `{"username":"alice","password":"wrong-password"}`).Code)
	firstAccess, firstRefresh := login()
	otherAccess, otherRefresh := login()
	require.NotEqual(t, firstAccess, otherAccess)
	require.NotEqual(t, firstRefresh, otherRefresh)
	nextAccess, nextRefresh := credentials(request("/refresh", firstRefresh, ""))
	response := request("/refresh", firstRefresh, "")
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Equal(t, -1, response.Result().Cookies()[0].MaxAge)
	userClient := sso.NewUserAPIClient(conn)
	_, err = userClient.VerifyAccessToken(rpcCtx, &sso.VerifyTokenRequest{AccessToken: nextAccess})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.Equal(t, http.StatusUnauthorized, request("/refresh", nextRefresh, "").Code)
	_, err = userClient.VerifyAccessToken(rpcCtx, &sso.VerifyTokenRequest{AccessToken: otherAccess})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, request("/logout", otherRefresh, "").Code)
	_, err = userClient.VerifyAccessToken(rpcCtx, &sso.VerifyTokenRequest{AccessToken: otherAccess})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.Equal(t, http.StatusOK, request("/logout", otherRefresh, "").Code)
	require.Equal(t, http.StatusOK, request("/logout", "", "").Code)
	// Concurrent HTTP refreshes traverse the same RPC/storage policy.
	_, racedRefresh := login()
	responses := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() { responses <- request("/refresh", racedRefresh, "") }()
	}
	successes := 0
	var racedAccess string
	for range 2 {
		response := <-responses
		if response.Code == http.StatusOK {
			successes++
			racedAccess, _ = credentials(response)
		} else {
			require.Equal(t, http.StatusUnauthorized, response.Code)
		}
	}
	require.Equal(t, 1, successes)
	_, err = userClient.VerifyAccessToken(rpcCtx, &sso.VerifyTokenRequest{AccessToken: racedAccess})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	// Exact idle expiry must reject through the RPC boundary as well.
	idleAccess, idleRefresh := login()
	now = now.Add(7 * 24 * time.Hour)
	_, err = userClient.VerifyAccessToken(rpcCtx, &sso.VerifyTokenRequest{AccessToken: idleAccess})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.Equal(t, http.StatusUnauthorized, request("/refresh", idleRefresh, "").Code)
	// Dependency failures preserve the cookie and expose only a safe availability error.
	now = time.Now().UTC().Truncate(time.Microsecond)
	_, liveRefresh := login()
	db.Close()
	response = request("/refresh", liveRefresh, "")
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Empty(t, response.Result().Cookies())
	require.NotContains(t, response.Body.String(), liveRefresh)
	require.Equal(t, http.StatusServiceUnavailable, request("/login", "", `{"username":"alice","password":"secret123"}`).Code)
	_, err = userClient.VerifyAccessToken(rpcCtx, &sso.VerifyTokenRequest{AccessToken: idleAccess})
	require.Equal(t, codes.Unavailable, status.Code(err))
}
