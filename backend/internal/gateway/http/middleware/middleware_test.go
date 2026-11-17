package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/http/middleware"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	gatewaymiddlewaremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/gateway/http/middleware"
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

func TestAuthMiddlewaresPlaceTypedScopeInRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	user := model.AuthUser{UserID: userID.String(), Username: "alice", Role: "admin"}

	for _, tt := range authMiddlewareCases() {
		t.Run(tt.name, func(t *testing.T) {
			verifier := gatewaymiddlewaremocks.NewMockTokenVerifier(t)
			tt.expectUser(verifier, user)

			var scope accessscope.Scope
			var hasScope bool
			router := gin.New()
			router.GET("/", tt.middleware(verifier), func(c *gin.Context) {
				scope, hasScope = accessscope.FromContext(c.Request.Context())
				c.Status(http.StatusNoContent)
			})

			resp := request(router, http.MethodGet, "/", "", map[string]string{"Authorization": "Bearer token"})

			require.Equal(t, http.StatusNoContent, resp.Code)
			require.True(t, hasScope)
			require.Equal(t, accessscope.Scope{Kind: tt.kind, UserID: userID, Username: "alice", Role: "admin"}, scope)
		})
	}
}

func TestAuthMiddlewaresRejectInvalidUserIDWithUnauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tt := range authMiddlewareCases() {
		t.Run(tt.name, func(t *testing.T) {
			verifier := gatewaymiddlewaremocks.NewMockTokenVerifier(t)
			tt.expectUser(verifier, model.AuthUser{UserID: "not-a-uuid"})

			router := gin.New()
			router.GET("/", tt.middleware(verifier), func(c *gin.Context) {
				t.Error("handler must not be called")
			})

			resp := request(router, http.MethodGet, "/", "", map[string]string{"Authorization": "Bearer token"})

			require.Equal(t, http.StatusUnauthorized, resp.Code)
		})
	}
}

type authMiddlewareCase struct {
	name       string
	kind       accessscope.Kind
	middleware func(middleware.TokenVerifier) gin.HandlerFunc
	expectUser func(*gatewaymiddlewaremocks.MockTokenVerifier, model.AuthUser)
}

func authMiddlewareCases() []authMiddlewareCase {
	const permission = "system.config.read"
	return []authMiddlewareCase{
		{
			name:       "auth",
			kind:       accessscope.KindUser,
			middleware: middleware.Auth,
			expectUser: func(verifier *gatewaymiddlewaremocks.MockTokenVerifier, user model.AuthUser) {
				verifier.EXPECT().VerifyAccessToken(mock.Anything, "Bearer token").Return(user, nil)
			},
		},
		{
			name: "require permission",
			kind: accessscope.KindAdmin,
			middleware: func(verifier middleware.TokenVerifier) gin.HandlerFunc {
				return middleware.RequirePermission(verifier, permission)
			},
			expectUser: func(verifier *gatewaymiddlewaremocks.MockTokenVerifier, user model.AuthUser) {
				verifier.EXPECT().CheckPermission(mock.Anything, "Bearer token", permission).Return(user, nil)
			},
		},
	}
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

func TestRateLimiterKeepsLongWindowBucketAfterShortWindowTraffic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := middleware.NewRateLimiter()
		allowUpload := func() bool { return limiter.Allow("upload:203.0.113.7", 1, time.Hour) }
		allowAuth := func() bool { return limiter.Allow("auth:203.0.113.7", 10, time.Minute) }

		require.True(t, allowUpload())
		require.False(t, allowUpload())

		time.Sleep(10 * time.Minute)
		require.True(t, allowAuth())

		require.False(t, allowUpload(), "upload bucket must hold for its whole hour")

		time.Sleep(time.Hour)
		require.True(t, allowUpload(), "upload bucket must reset after its window")
	})
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
