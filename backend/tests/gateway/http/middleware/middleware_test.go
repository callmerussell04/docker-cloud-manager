package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/middleware"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	gatewaymiddlewaremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/gateway/http/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAuthInjectsUserScopeAndRejectsVerifierErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.NewString()
	verifier := gatewaymiddlewaremocks.NewMockTokenVerifier(t)
	router := gin.New()
	router.GET("/me", middleware.Auth(verifier), func(c *gin.Context) {
		scope, ok := accessscope.FromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, accessscope.KindUser, scope.Kind)
		require.Equal(t, userID, c.GetString("user_id"))
		c.Status(http.StatusNoContent)
	})
	router.GET("/denied", middleware.Auth(verifier), func(c *gin.Context) {
		t.Fatal("handler must not be called")
	})

	verifier.EXPECT().
		VerifyAccessToken(mock.Anything, "Bearer token").
		Return(model.AuthUser{UserID: userID, Username: "alice", Role: "user"}, nil)
	resp := request(router, http.MethodGet, "/me", "", map[string]string{"Authorization": "Bearer token"})
	require.Equal(t, http.StatusNoContent, resp.Code)

	verifier.EXPECT().
		VerifyAccessToken(mock.Anything, "Bearer bad").
		Return(model.AuthUser{}, apperrors.ErrUnauthorized)
	resp = request(router, http.MethodGet, "/denied", "", map[string]string{"Authorization": "Bearer bad"})
	require.Equal(t, http.StatusUnauthorized, resp.Code)
}

func TestRequirePermissionInjectsAdminScopeAndRejectsInvalidUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminID := uuid.NewString()
	verifier := gatewaymiddlewaremocks.NewMockTokenVerifier(t)
	router := gin.New()
	router.GET("/admin", middleware.RequirePermission(verifier, "system.config.read"), func(c *gin.Context) {
		scope, ok := accessscope.FromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, accessscope.KindAdmin, scope.Kind)
		require.Equal(t, adminID, c.GetString("user_id"))
		c.Status(http.StatusNoContent)
	})
	router.GET("/invalid-user", middleware.RequirePermission(verifier, "system.config.read"), func(c *gin.Context) {
		t.Fatal("handler must not be called")
	})

	verifier.EXPECT().
		CheckPermission(mock.Anything, "Bearer admin", "system.config.read").
		Return(model.AuthUser{UserID: adminID, Username: "root", Role: "admin"}, nil)
	resp := request(router, http.MethodGet, "/admin", "", map[string]string{"Authorization": "Bearer admin"})
	require.Equal(t, http.StatusNoContent, resp.Code)

	verifier.EXPECT().
		CheckPermission(mock.Anything, "Bearer broken", "system.config.read").
		Return(model.AuthUser{UserID: "not-a-uuid"}, nil)
	resp = request(router, http.MethodGet, "/invalid-user", "", map[string]string{"Authorization": "Bearer broken"})
	require.Equal(t, http.StatusUnauthorized, resp.Code)
}

func TestBodySizeLimitAppliesOnlyJSONMutatingRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.BodySizeLimit(middleware.BodyLimitConfig{MaxJSONBodyBytes: 4}))
	router.POST("/json", readBodyHandler)
	router.GET("/json", readBodyHandler)
	router.POST("/text", readBodyHandler)

	resp := request(router, http.MethodPost, "/json", "12345", map[string]string{"Content-Type": "application/json"})
	require.Equal(t, http.StatusRequestEntityTooLarge, resp.Code)

	resp = request(router, http.MethodGet, "/json", "12345", map[string]string{"Content-Type": "application/json"})
	require.Equal(t, http.StatusOK, resp.Code)
	require.Equal(t, "12345", resp.Body.String())

	resp = request(router, http.MethodPost, "/text", "12345", map[string]string{"Content-Type": "text/plain"})
	require.Equal(t, http.StatusOK, resp.Code)
	require.Equal(t, "12345", resp.Body.String())
}

func TestRateLimitAllowsWithinLimitRejectsOverLimitAndDisabledBypasses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	limiter := middleware.NewRateLimiter()
	router := gin.New()
	router.GET("/limited", middleware.RateLimit(limiter, middleware.RateLimitConfig{Requests: 1, Window: time.Minute}, "auth"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	router.GET("/disabled", middleware.RateLimit(limiter, middleware.RateLimitConfig{}, "disabled"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	resp := request(router, http.MethodGet, "/limited", "", nil)
	require.Equal(t, http.StatusNoContent, resp.Code)
	resp = request(router, http.MethodGet, "/limited", "", nil)
	require.Equal(t, http.StatusTooManyRequests, resp.Code)

	for i := 0; i < 3; i++ {
		resp = request(router, http.MethodGet, "/disabled", "", nil)
		require.Equal(t, http.StatusNoContent, resp.Code)
	}
}

func TestCORSMiddlewareHandlesAllowedDeniedAndEmptyOrigins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.CORSMiddleware([]string{"http://frontend.example"}))
	router.GET("/resource", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	resp := request(router, http.MethodOptions, "/resource", "", map[string]string{
		"Origin":                         "http://frontend.example",
		"Access-Control-Request-Method":  "GET",
		"Access-Control-Request-Headers": "Authorization",
	})
	require.Equal(t, http.StatusNoContent, resp.Code)
	require.Equal(t, "http://frontend.example", resp.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "true", resp.Header().Get("Access-Control-Allow-Credentials"))

	resp = request(router, http.MethodOptions, "/resource", "", map[string]string{
		"Origin":                        "http://evil.example",
		"Access-Control-Request-Method": "GET",
	})
	require.Equal(t, http.StatusForbidden, resp.Code)
	require.Empty(t, resp.Header().Get("Access-Control-Allow-Origin"))

	resp = request(router, http.MethodGet, "/resource", "", nil)
	require.Equal(t, http.StatusNoContent, resp.Code)
	require.Empty(t, resp.Header().Get("Access-Control-Allow-Origin"))
}

func readBodyHandler(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.Status(http.StatusRequestEntityTooLarge)
		return
	}
	c.String(http.StatusOK, string(body))
}

func request(router http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
