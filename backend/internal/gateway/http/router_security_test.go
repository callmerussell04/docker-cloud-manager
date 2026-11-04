package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gatewayhttp "github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/http"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/http/middleware"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/permissions"
	gatewayhandlermocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/gateway/http/handler"
)

// adminRoutePermissions is the expected permission of every admin route. The
// two admin stream routes are authenticated by a stream ticket instead; their
// ticket comes from the admin ticket route named in ticketRoute.
var adminRoutePermissions = []struct {
	method      string
	route       string
	permission  string
	ticketRoute string
}{
	{method: http.MethodGet, route: "/api/v1/admin/config", permission: permissions.SystemConfigRead},
	{method: http.MethodPut, route: "/api/v1/admin/config", permission: permissions.SystemConfigUpdate},
	{method: http.MethodGet, route: "/api/v1/admin/monitoring", permission: permissions.SystemMonitoringRead},
	{method: http.MethodGet, route: "/api/v1/admin/reports/overview", permission: permissions.ReportsAdminRead},
	{method: http.MethodGet, route: "/api/v1/admin/reports/users", permission: permissions.ReportsAdminRead},
	{method: http.MethodGet, route: "/api/v1/admin/reports/users/:id/usage", permission: permissions.ReportsAdminRead},
	{method: http.MethodGet, route: "/api/v1/admin/reports/audit-events", permission: permissions.ReportsAdminRead},
	{method: http.MethodPost, route: "/api/v1/admin/reports/usage-snapshots/refresh", permission: permissions.ReportsAdminRead},
	{method: http.MethodGet, route: "/api/v1/admin/users", permission: permissions.UsersAdminList},
	{method: http.MethodGet, route: "/api/v1/admin/users/:id", permission: permissions.UsersAdminRead},
	{method: http.MethodPost, route: "/api/v1/admin/users", permission: permissions.UsersAdminCreate},
	{method: http.MethodPut, route: "/api/v1/admin/users/:id", permission: permissions.UsersAdminUpdate},
	{method: http.MethodDelete, route: "/api/v1/admin/users/:id", permission: permissions.UsersAdminDelete},
	{method: http.MethodPost, route: "/api/v1/admin/users/:id/activate", permission: permissions.UsersAdminUpdate},
	{method: http.MethodGet, route: "/api/v1/admin/containers", permission: permissions.ContainersAdminList},
	{method: http.MethodPost, route: "/api/v1/admin/containers/:id/action/:action", permission: permissions.ContainersAdminAction},
	{method: http.MethodGet, route: "/api/v1/admin/containers/:id/stats", permission: permissions.ContainersAdminStats},
	{method: http.MethodPost, route: "/api/v1/admin/containers/:id/logs/stream/ticket", permission: permissions.ContainersAdminLogs},
	{method: http.MethodPost, route: "/api/v1/admin/containers/:id/terminal/ticket", permission: permissions.ContainersAdminTerminal},
	{method: http.MethodGet, route: "/api/v1/admin/containers/:id/logs/stream", ticketRoute: "/api/v1/admin/containers/:id/logs/stream/ticket"},
	{method: http.MethodGet, route: "/api/v1/admin/containers/:id/terminal", ticketRoute: "/api/v1/admin/containers/:id/terminal/ticket"},
	{method: http.MethodGet, route: "/api/v1/admin/volumes", permission: permissions.VolumesAdminList},
	{method: http.MethodDelete, route: "/api/v1/admin/volumes/:id", permission: permissions.VolumesAdminDelete},
	{method: http.MethodGet, route: "/api/v1/admin/images", permission: permissions.ImagesAdminList},
	{method: http.MethodDelete, route: "/api/v1/admin/images/:id", permission: permissions.ImagesAdminDelete},
	{method: http.MethodGet, route: "/api/v1/admin/builds", permission: permissions.BuildsAdminList},
	{method: http.MethodGet, route: "/api/v1/admin/builds/:id/logs", permission: permissions.BuildsAdminList},
	{method: http.MethodPost, route: "/api/v1/admin/builds/:id/cancel", permission: permissions.BuildsAdminDelete},
	{method: http.MethodDelete, route: "/api/v1/admin/builds/:id", permission: permissions.BuildsAdminDelete},
	{method: http.MethodGet, route: "/api/v1/admin/projects", permission: permissions.ProjectsAdminList},
	{method: http.MethodDelete, route: "/api/v1/admin/projects/:id", permission: permissions.ProjectsAdminDelete},
	{method: http.MethodPost, route: "/api/v1/admin/projects/:id/start", permission: permissions.ProjectsAdminStart},
	{method: http.MethodPost, route: "/api/v1/admin/projects/:id/stop", permission: permissions.ProjectsAdminStop},
	{method: http.MethodPost, route: "/api/v1/admin/projects/:id/cancel", permission: permissions.ProjectsAdminStop},
}

func TestRouterAdminRouteTableCoversEveryAdminRoute(t *testing.T) {
	h := newSecurityHarness(t, nil)
	table := make(map[string]struct{}, len(adminRoutePermissions))
	for _, row := range adminRoutePermissions {
		table[row.method+" "+row.route] = struct{}{}
	}
	registered := make(map[string]struct{})
	for _, route := range h.router.Routes() {
		if !strings.HasPrefix(route.Path, "/api/v1/admin/") {
			continue
		}
		key := route.Method + " " + route.Path
		registered[key] = struct{}{}
		require.Contains(t, table, key, "admin route has no expected permission in adminRoutePermissions")
	}
	for key := range table {
		require.Contains(t, registered, key, "adminRoutePermissions lists a route the router does not register")
	}
}

func TestRouterAdminRoutesRequireTheirPermission(t *testing.T) {
	for _, row := range adminRoutePermissions {
		if row.ticketRoute != "" {
			continue
		}
		t.Run(row.method+" "+row.route, func(t *testing.T) {
			h := newSecurityHarness(t, nil)
			path := concretePath(row.route, uuid.NewString())

			rec := h.serve(row.method, path, nil)
			require.Equal(t, http.StatusUnauthorized, rec.Code, "no token: %s", rec.Body)

			h.verifier.grant("Bearer lacking", accessIdentity("admin", func(permission string) bool { return permission != row.permission }))
			rec = h.serve(row.method, path, map[string]string{"Authorization": "Bearer lacking"})
			require.Equal(t, http.StatusForbidden, rec.Code, "token lacking %s: %s", row.permission, rec.Body)

			h.verifier.grant("Bearer granted", accessIdentity("admin", func(permission string) bool { return permission == row.permission }))
			rec = h.serve(row.method, path, map[string]string{"Authorization": "Bearer granted"})
			requirePassedAuthorization(t, rec)
		})
	}
}

func TestRouterAdminStreamRoutesRequireAnAdminTicket(t *testing.T) {
	for _, row := range adminRoutePermissions {
		if row.ticketRoute == "" {
			continue
		}
		t.Run(row.method+" "+row.route, func(t *testing.T) {
			h := newSecurityHarness(t, nil)
			containerID := uuid.NewString()
			path := concretePath(row.route, containerID)

			rec := h.serve(row.method, path, nil)
			require.Equal(t, http.StatusUnauthorized, rec.Code, "no ticket: %s", rec.Body)

			userTicketRoute := strings.Replace(row.ticketRoute, "/api/v1/admin/", "/api/v1/", 1)
			userTicket := h.issueTicket(t, concretePath(userTicketRoute, containerID), accessIdentity("user", nil))
			rec = h.serve(row.method, path+"?ticket="+userTicket, nil)
			require.Equal(t, http.StatusForbidden, rec.Code, "user-route ticket: %s", rec.Body)

			adminTicket := h.issueTicket(t, concretePath(row.ticketRoute, containerID), accessIdentity("admin", func(string) bool { return true }))
			rec = h.serve(row.method, path+"?ticket="+adminTicket, nil)
			requirePassedAuthorization(t, rec)
			require.Len(t, h.upstream.requests(), 1, "admin ticket must reach Telemetry")
		})
	}
}

// spoofedIdentityHeaders are the client-supplied identity headers Gateway
// strips before proxying, each with a value only a client could have sent.
var spoofedIdentityHeaders = map[string]string{
	"Authorization":             "Bearer spoofed-authorization",
	"Cookie":                    "refresh_token=spoofed-cookie",
	internalauth.HeaderScope:    "admin",
	internalauth.HeaderUserID:   "6f1c2a1e-0000-4000-8000-5900005900aa",
	internalauth.HeaderUsername: "spoofed-username",
	internalauth.HeaderRole:     "spoofed-role",
	internalauth.HeaderName:     "spoofed-internal-token",
	"X-Forwarded-User":          "spoofed-forwarded-user",
	"X-Forwarded-Email":         "spoofed@example.test",
	"X-Forwarded-Groups":        "spoofed-admins",
	"X-Remote-User":             "spoofed-remote-user",
}

func TestRouterProxiesNeverForwardSpoofedIdentityHeaders(t *testing.T) {
	for _, proxy := range []struct {
		name string
		// path is the proxied route; ticketRoute, when set, issues the stream ticket it needs.
		path        string
		ticketRoute string
	}{
		{name: "Core proxy", path: "/api/v1/builds/:id/logs"},
		{name: "logs stream proxy", path: "/api/v1/containers/:id/logs/stream", ticketRoute: "/api/v1/containers/:id/logs/stream/ticket"},
		{name: "terminal stream proxy", path: "/api/v1/containers/:id/terminal", ticketRoute: "/api/v1/containers/:id/terminal/ticket"},
	} {
		for _, scope := range []struct {
			name string
			// user is the authenticated actor; nil sends the request without a
			// Gateway credential, so no scope reaches the context.
			user *model.AuthUser
		}{
			{name: "no scope"},
			{name: "user scope", user: &model.AuthUser{UserID: uuid.NewString(), Username: "alice", Role: "user"}},
			// Gateway sets X-Username and X-User-Role only when the scope has them,
			// so only stripping keeps the spoofed values out here.
			{name: "user scope without username or role", user: &model.AuthUser{UserID: uuid.NewString()}},
		} {
			t.Run(proxy.name+"/"+scope.name, func(t *testing.T) {
				h := newSecurityHarness(t, nil)
				id := uuid.NewString()
				path := concretePath(proxy.path, id)
				headers := make(map[string]string, len(spoofedIdentityHeaders))
				for key, value := range spoofedIdentityHeaders {
					headers[key] = value
				}
				if scope.user != nil {
					if proxy.ticketRoute == "" {
						// Core proxy routes authenticate the bearer token, which is itself
						// a credential Core must never receive.
						h.verifier.grant(spoofedIdentityHeaders["Authorization"], fakeIdentity{user: *scope.user})
					} else {
						path += "?ticket=" + h.issueTicket(t, concretePath(proxy.ticketRoute, id), fakeIdentity{user: *scope.user})
					}
				}

				rec := h.serve(http.MethodGet, path, headers)

				upstream := h.upstream.requests()
				for _, req := range upstream {
					for key, spoofed := range spoofedIdentityHeaders {
						require.NotContains(t, req.Header.Values(key), spoofed, "spoofed %s reached the upstream", key)
					}
				}
				if scope.user == nil {
					require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
					require.Empty(t, upstream, "an unauthenticated request must not reach the upstream")
					return
				}
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				require.Len(t, upstream, 1)
				forwarded := upstream[0].Header
				require.Equal(t, "user", forwarded.Get(internalauth.HeaderScope))
				require.Equal(t, scope.user.UserID, forwarded.Get(internalauth.HeaderUserID))
				require.Equal(t, scope.user.Username, forwarded.Get(internalauth.HeaderUsername))
				require.Equal(t, scope.user.Role, forwarded.Get(internalauth.HeaderRole))
				require.Equal(t, securityInternalToken, forwarded.Get(internalauth.HeaderName))
				require.Empty(t, upstream[0].URL.Query().Get("ticket"), "the stream ticket must not reach Telemetry")
			})
		}
	}
}

func TestRouterKeepsCredentialsOutOfLogs(t *testing.T) {
	const (
		accessToken   = "Bearer secret-access-token-0d6c"
		rejectedToken = "Bearer secret-rejected-token-91fa"
		refreshCookie = "secret-refresh-cookie-7b2e"
	)
	var logs bytes.Buffer
	h := newSecurityHarness(t, slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	h.verifier.grant(accessToken, accessIdentity("user", nil))
	h.auth.EXPECT().Refresh(mock.Anything, refreshCookie).Return(model.Tokens{}, apperrors.ErrInvalidToken).Once()
	containerID := uuid.NewString()
	credentials := map[string]string{"Authorization": accessToken, "Cookie": "refresh_token=" + refreshCookie}

	logsTicket := h.issueTicket(t, concretePath("/api/v1/containers/:id/logs/stream/ticket", containerID), accessIdentity("user", nil))
	terminalTicket := h.issueTicket(t, concretePath("/api/v1/containers/:id/terminal/ticket", containerID), accessIdentity("user", nil))
	streamPath := concretePath("/api/v1/containers/:id/logs/stream", containerID) + "?ticket=" + logsTicket
	requests := []struct {
		method  string
		path    string
		headers map[string]string
		want    int
	}{
		{method: http.MethodGet, path: concretePath("/api/v1/containers/:id/terminal", containerID) + "?ticket=" + terminalTicket, headers: credentials, want: http.StatusOK},
		{method: http.MethodGet, path: streamPath, headers: credentials, want: http.StatusOK},
		{method: http.MethodGet, path: streamPath, headers: credentials, want: http.StatusUnauthorized},
		{method: http.MethodGet, path: concretePath("/api/v1/builds/:id/logs", uuid.NewString()), headers: credentials, want: http.StatusOK},
		{method: http.MethodGet, path: "/api/v1/stats", headers: map[string]string{"Authorization": rejectedToken, "Cookie": "refresh_token=" + refreshCookie}, want: http.StatusUnauthorized},
		{method: http.MethodPost, path: "/api/v1/auth/refresh", headers: map[string]string{"Origin": securityOrigin, "Cookie": "refresh_token=" + refreshCookie}, want: http.StatusUnauthorized},
	}
	for _, req := range requests {
		rec := h.serve(req.method, req.path, req.headers)
		require.Equal(t, req.want, rec.Code, "%s %s: %s", req.method, req.path, rec.Body)
	}

	records := decodeLogRecords(t, &logs)
	completed := 0
	for _, record := range records {
		if record["msg"] == "http request completed" {
			completed++
		}
	}
	// One access log per request, including the two tickets issued during setup.
	require.Equal(t, len(requests)+2, completed, "every request must be logged, or this test proves nothing")
	for _, secret := range []string{logsTicket, terminalTicket, strings.TrimPrefix(accessToken, "Bearer "), strings.TrimPrefix(rejectedToken, "Bearer "), refreshCookie} {
		require.NotContains(t, logs.String(), secret)
	}
}

const (
	securityInternalToken = "internal-token-value"
	securityOrigin        = "https://dcm.example.test"
)

type securityHarness struct {
	router   *gin.Engine
	verifier *fakeTokenVerifier
	auth     *gatewayhandlermocks.MockAuthService
	upstream *recordingUpstream
}

// newSecurityHarness builds the production router with real proxies, a real
// ticket store and a recording upstream. Core and user-management services
// are unset: a request that reaches one passed authorization, and the
// router's recovery middleware answers it with 500.
func newSecurityHarness(t *testing.T, logger *slog.Logger) *securityHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	h := &securityHarness{
		verifier: &fakeTokenVerifier{identities: make(map[string]fakeIdentity)},
		auth:     gatewayhandlermocks.NewMockAuthService(t),
		upstream: &recordingUpstream{},
	}
	tickets := service.NewTelemetryTicketStore(0)
	proxyOpts := handler.ProxyOptions{InternalToken: securityInternalToken, Transport: h.upstream, AllowedOrigins: []string{securityOrigin}}
	coreOpts := proxyOpts
	coreOpts.TargetURL = "http://core.internal"
	telemetryOpts := proxyOpts
	telemetryOpts.TargetURL = "http://telemetry.internal"

	coreProxy, err := handler.NewCoreProxyHandler(coreOpts)
	require.NoError(t, err)
	streamProxy := func(newProxy func(handler.ProxyOptions, handler.TelemetryProxyAuthOptions) (gin.HandlerFunc, error), streamType string, adminRoute bool) gin.HandlerFunc {
		proxy, err := newProxy(telemetryOpts, handler.TelemetryProxyAuthOptions{Tickets: tickets, StreamType: streamType, AdminRoute: adminRoute})
		require.NoError(t, err)
		return proxy
	}

	cfg := gatewayhttp.DefaultConfig()
	cfg.AuthRateLimit = middleware.RateLimitConfig{}
	cfg.UploadRateLimit = middleware.RateLimitConfig{}
	// Production derives the refresh-cookie origins from the CORS origins.
	cfg.CORSAllowedOrigins = []string{securityOrigin}
	h.router, err = gatewayhttp.NewRouter(cfg, gatewayhttp.RouterDeps{
		AuthHandler:                 handler.NewAuthHandler(h.auth, handler.CookieConfig{Name: "refresh_token"}, handler.AuthRedirectConfig{CookieAllowedOrigins: []string{securityOrigin}}),
		CoreHandler:                 handler.NewCoreHandler(unreachableCoreService{}),
		UserHandler:                 handler.NewUserManagementHandler(unreachableUserService{}),
		HealthHandler:               handler.NewHealthHandler(nil),
		TelemetryTicketHandler:      handler.NewTelemetryTicketHandler(tickets),
		CoreHTTPProxy:               coreProxy,
		TelemetryLogsProxy:          streamProxy(handler.NewTelemetryLogsProxyHandler, model.TelemetryStreamLogs, false),
		TelemetryTerminalProxy:      streamProxy(handler.NewTelemetryTerminalProxyHandler, model.TelemetryStreamTerminal, false),
		AdminTelemetryLogsProxy:     streamProxy(handler.NewTelemetryLogsProxyHandler, model.TelemetryStreamLogs, true),
		AdminTelemetryTerminalProxy: streamProxy(handler.NewTelemetryTerminalProxyHandler, model.TelemetryStreamTerminal, true),
		TokenVerifier:               h.verifier,
		Logger:                      logger,
	})
	require.NoError(t, err)
	return h
}

// serve records the response through a writer that supports CloseNotify,
// which the reverse proxies need and httptest.ResponseRecorder lacks.
func (h *securityHarness) serve(method, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(closeNotifyRecorder{ResponseRecorder: rec}, req)
	return rec
}

type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
}

func (closeNotifyRecorder) CloseNotify() <-chan bool {
	return make(chan bool)
}

// issueTicket obtains a stream ticket through the router's ticket route,
// authenticated as identity.
func (h *securityHarness) issueTicket(t *testing.T, ticketPath string, identity fakeIdentity) string {
	t.Helper()
	token := "Bearer ticket-issuer-" + uuid.NewString()
	h.verifier.grant(token, identity)
	rec := h.serve(http.MethodPost, ticketPath, map[string]string{"Authorization": token})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var body struct {
		Ticket string `json:"ticket"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.NotEmpty(t, body.Ticket)
	return body.Ticket
}

func requirePassedAuthorization(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	require.NotContains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, rec.Code, "request was not let through: %s", rec.Body)
}

func concretePath(route, id string) string {
	return strings.NewReplacer(":id", id, ":action", "start").Replace(route)
}

// accessIdentity is a new actor with role, holding the permissions allows accepts.
func accessIdentity(role string, allows func(permission string) bool) fakeIdentity {
	return fakeIdentity{
		user:   model.AuthUser{UserID: uuid.NewString(), Username: role + "-name", Role: role},
		allows: allows,
	}
}

func decodeLogRecords(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(logs.Bytes()))
	for decoder.More() {
		var record map[string]any
		require.NoError(t, decoder.Decode(&record))
		records = append(records, record)
	}
	return records
}

type fakeIdentity struct {
	user   model.AuthUser
	allows func(permission string) bool
}

// fakeTokenVerifier answers like SSO: an unknown token is unauthorized, and a
// known token without the permission is forbidden. It is a fake rather than
// the generated mock because the outcome under test is the router's answer
// for whichever tokens a case grants, not the verifier calls themselves.
type fakeTokenVerifier struct {
	mu         sync.Mutex
	identities map[string]fakeIdentity
}

func (v *fakeTokenVerifier) grant(authHeader string, identity fakeIdentity) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.identities[authHeader] = identity
}

func (v *fakeTokenVerifier) lookup(authHeader string) (fakeIdentity, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	identity, ok := v.identities[authHeader]
	if !ok || authHeader == "" {
		return fakeIdentity{}, apperrors.ErrUnauthorized
	}
	return identity, nil
}

func (v *fakeTokenVerifier) VerifyAccessToken(_ context.Context, authHeader string) (model.AuthUser, error) {
	identity, err := v.lookup(authHeader)
	return identity.user, err
}

func (v *fakeTokenVerifier) CheckPermission(_ context.Context, authHeader, permission string) (model.AuthUser, error) {
	identity, err := v.lookup(authHeader)
	if err != nil {
		return model.AuthUser{}, err
	}
	if identity.allows == nil || !identity.allows(permission) {
		return model.AuthUser{}, apperrors.ErrForbidden
	}
	return identity.user, nil
}

// recordingUpstream stands in for Core and Telemetry and keeps every request
// the Gateway proxies forward.
type recordingUpstream struct {
	mu       sync.Mutex
	received []*http.Request
}

func (u *recordingUpstream) RoundTrip(req *http.Request) (*http.Response, error) {
	u.mu.Lock()
	u.received = append(u.received, req.Clone(context.Background()))
	u.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Request:    req,
	}, nil
}

func (u *recordingUpstream) requests() []*http.Request {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]*http.Request(nil), u.received...)
}

type unreachableCoreService struct{ handler.CoreService }

type unreachableUserService struct{ handler.UserManagementService }
