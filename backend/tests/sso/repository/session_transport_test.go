package repository_test

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
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/grpc/client"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/http/handler"
	gatewayservice "github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/internalauth"
	ssogrpc "github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/grpc"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/jwt"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/tests/testutil/dbtest"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

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
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.UnaryInterceptor(internalauth.UnaryServerInterceptor("internal-secret")))
	ssogrpc.Register(server, svc)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///sso", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithUnaryInterceptor(internalauth.UnaryClientInterceptor("internal-secret")))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	ssoClient := client.NewSSOClient(conn)
	auth := handler.NewAuthHandler(gatewayservice.NewAuth(ssoClient), handler.CookieConfig{Name: "refresh_token", HTTPOnly: true, Path: "/api/v1/auth"}, handler.AuthRedirectConfig{CookieAllowedOrigins: []string{"http://frontend.example"}})
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
	_, err = userClient.VerifyAccessToken(ctx, &sso.VerifyTokenRequest{AccessToken: nextAccess})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.Equal(t, http.StatusUnauthorized, request("/refresh", nextRefresh, "").Code)
	_, err = userClient.VerifyAccessToken(ctx, &sso.VerifyTokenRequest{AccessToken: otherAccess})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, request("/logout", otherRefresh, "").Code)
	_, err = userClient.VerifyAccessToken(ctx, &sso.VerifyTokenRequest{AccessToken: otherAccess})
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
	_, err = userClient.VerifyAccessToken(ctx, &sso.VerifyTokenRequest{AccessToken: racedAccess})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	// Exact idle expiry must reject through the RPC boundary as well.
	idleAccess, idleRefresh := login()
	now = now.Add(7 * 24 * time.Hour)
	_, err = userClient.VerifyAccessToken(ctx, &sso.VerifyTokenRequest{AccessToken: idleAccess})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.Equal(t, http.StatusUnauthorized, request("/refresh", idleRefresh, "").Code)
	// Dependency failures preserve the cookie and expose only a safe availability error.
	now = time.Now().UTC().Truncate(time.Microsecond)
	_, liveRefresh := login()
	require.NoError(t, db.Close())
	response = request("/refresh", liveRefresh, "")
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Empty(t, response.Result().Cookies())
	require.NotContains(t, response.Body.String(), liveRefresh)
	require.Equal(t, http.StatusServiceUnavailable, request("/login", "", `{"username":"alice","password":"secret123"}`).Code)
	_, err = userClient.VerifyAccessToken(ctx, &sso.VerifyTokenRequest{AccessToken: idleAccess})
	require.Equal(t, codes.Unavailable, status.Code(err))
}
