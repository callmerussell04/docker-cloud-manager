package session_flow_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/api/sso"
	coreclient "github.com/callmerussell04/docker-cloud-manager/backend/internal/core/grpc/client"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/grpc/client"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/http/handler"
	gatewaymodel "github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/internalauth"
	ssogrpc "github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/grpc"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/jwt"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/oidc"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	gatewayhandlermocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/oidctest"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"
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
	svc := service.NewAuthServiceWithConfig(repository.NewUserRepository(db), jwt.NewProvider("secret"), repository.NewSessionRepository(db), service.OIDCDeps{}, service.Config{
		LocalLoginEnabled: true, LocalRegisterEnabled: true,
	})
	aliceID, err := svc.Register(ctx, "alice", "alice@example.com", "secret123")
	require.NoError(t, err)
	tokens, err := loginTokens(svc.Login(ctx, "alice", "secret123"))
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
	svc := service.NewAuthServiceWithConfig(repository.NewUserRepository(db), jwt.NewProvider("secret"), repository.NewSessionRepository(db), service.OIDCDeps{}, service.Config{
		LocalLoginEnabled: true, LocalRegisterEnabled: true, Now: func() time.Time { return now },
	})
	_, err := svc.Register(ctx, "alice", "alice@example.com", "secret123")
	require.NoError(t, err)
	conn := serveSSO(t, svc)(internalauth.UnaryClientInterceptor(gatewayCredential))
	ssoClient := client.NewSSOClient(conn)
	// Direct token checks state system scope, as Gateway's client does.
	rpcCtx := accessscope.WithSystemScope(ctx)
	recorder, _ := auditMock(t)
	auth := handler.NewAuthHandler(ssoClient, recorder, handler.CookieConfig{Name: "refresh_token", HTTPOnly: true, Path: "/api/v1/auth"}, handler.AuthRedirectConfig{CookieAllowedOrigins: []string{"http://frontend.example"}})
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

const oidcCallbackPath = "/api/v1/auth/oidc/keycloak/callback"

// browser plays the user's browser against a Gateway router: it keeps the
// cookies Gateway sets and sends them back on paths they are scoped to.
type browser struct {
	t       *testing.T
	ctx     context.Context
	router  http.Handler
	cookies map[string]*http.Cookie
	audit   *auditTrail
}

// auditTrail holds the events Gateway sent to Core's audit API, each with
// the scope its call carried, which is where Core takes the actor from.
type auditTrail struct {
	mu     sync.Mutex
	events []auditedEvent
}

type auditedEvent struct {
	Scope accessscope.Scope
	Event gatewaymodel.AuditEvent
}

func (a *auditTrail) recorded() []auditedEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]auditedEvent(nil), a.events...)
}

// auditMock stands in for Core's audit API, accepting every event into the
// returned trail.
func auditMock(t *testing.T) (*gatewayhandlermocks.MockAuditRecorder, *auditTrail) {
	t.Helper()
	trail := &auditTrail{}
	recorder := gatewayhandlermocks.NewMockAuditRecorder(t)
	recorder.EXPECT().RecordAuditEvent(mock.Anything, mock.Anything).Run(func(ctx context.Context, event gatewaymodel.AuditEvent) {
		scope, _ := accessscope.FromContext(ctx)
		trail.mu.Lock()
		defer trail.mu.Unlock()
		trail.events = append(trail.events, auditedEvent{Scope: scope, Event: event})
	}).Return(nil).Maybe()
	return recorder, trail
}

func (b *browser) get(target string) *httptest.ResponseRecorder {
	return b.do(http.MethodGet, target)
}

func (b *browser) do(method, target string) *httptest.ResponseRecorder {
	b.t.Helper()
	req := httptest.NewRequest(method, target, nil).WithContext(b.ctx)
	req.Header.Set("Origin", "http://frontend.example")
	for _, cookie := range b.cookies {
		if strings.HasPrefix(req.URL.Path, cookie.Path) {
			req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
		}
	}
	response := httptest.NewRecorder()
	b.router.ServeHTTP(response, req)
	for _, cookie := range response.Result().Cookies() {
		if cookie.MaxAge < 0 {
			delete(b.cookies, cookie.Name)
			continue
		}
		b.cookies[cookie.Name] = cookie
	}
	return response
}

// oidcGateway serves svc behind SSO's caller policies and returns a browser
// against Gateway's auth routes at their production paths.
func oidcGateway(t *testing.T, ctx context.Context, svc *service.AuthService) (*browser, sso.UserAPIClient) {
	t.Helper()
	conn := serveSSO(t, svc)(internalauth.UnaryClientInterceptor(gatewayCredential))
	recorder, audit := auditMock(t)
	auth := handler.NewAuthHandler(client.NewSSOClient(conn), recorder, handler.CookieConfig{Name: "refresh_token", HTTPOnly: true, Path: "/api/v1/auth"}, handler.AuthRedirectConfig{
		AllowedOrigins:       []string{"http://frontend.example"},
		CookieAllowedOrigins: []string{"http://frontend.example"},
		CallbackPath:         "/auth/callback",
	})
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/auth/register", auth.Register)
	router.POST("/api/v1/auth/login", auth.Login)
	router.POST("/api/v1/auth/refresh", auth.Refresh)
	router.POST("/api/v1/auth/logout", auth.Logout)
	router.GET("/api/v1/auth/providers", auth.Providers)
	router.GET("/api/v1/auth/oidc/keycloak/start", auth.StartKeycloakLogin)
	router.GET(oidcCallbackPath, auth.CompleteKeycloakCallback)
	router.POST(backchannelLogoutPath, auth.KeycloakBackchannelLogout)
	return &browser{t: t, ctx: ctx, router: router, cookies: map[string]*http.Cookie{}, audit: audit}, sso.NewUserAPIClient(conn)
}

type providersAnswer struct {
	LocalLoginEnabled bool `json:"local_login_enabled"`
	OIDCProviders     []struct {
		Name      string `json:"name"`
		Available bool   `json:"available"`
	} `json:"oidc_providers"`
}

func providers(t *testing.T, b *browser) providersAnswer {
	t.Helper()
	response := b.get("/api/v1/auth/providers")
	require.Equal(t, http.StatusOK, response.Code)
	var answer providersAnswer
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &answer))
	return answer
}

const startPath = "/api/v1/auth/oidc/keycloak/start?redirect_after=http://frontend.example/auth/callback"

// SSO starts and serves local sign-in while the provider is unreachable, and
// once it reaches the provider a user signs in through Gateway, SSO and
// PostgreSQL against it.
func TestOIDCSignInThroughGatewayAfterTheProviderBecomesReachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db := dbtest.OpenSSOPostgres(t)
	idp := oidctest.New(t, oidctest.Options{Down: true, RedirectURL: "http://gateway.example" + oidcCallbackPath})
	provider, err := oidc.NewProvider(oidc.Config{
		IssuerURL: idp.Issuer(), ClientID: idp.ClientID(), ClientSecret: idp.ClientSecret(),
		RedirectURL: "http://gateway.example" + oidcCallbackPath,
	})
	require.NoError(t, err)
	require.Error(t, provider.Discover(ctx), "the provider starts unreachable")
	svc := service.NewAuthServiceWithConfig(repository.NewUserRepository(db), jwt.NewProvider("secret"), repository.NewSessionRepository(db), service.OIDCDeps{Provider: provider, LoginAttempts: repository.NewLoginAttemptRepository(db)}, service.Config{
		LocalLoginEnabled: true, LocalRegisterEnabled: true, OIDCEnabled: true, OIDCProviderName: "keycloak",
	})
	bobID, err := svc.Register(ctx, "bob", "bob@example.com", "secret123")
	require.NoError(t, err)
	b, users := oidcGateway(t, ctx, svc)
	rpcCtx := accessscope.WithSystemScope(ctx)

	// Registration and a failed local sign-in go through Gateway.
	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://frontend.example")
		recorded := httptest.NewRecorder()
		b.router.ServeHTTP(recorded, req)
		return recorded
	}
	registered := post("/api/v1/auth/register", `{"username":"carol","email":"carol@example.com","password":"carol-secret"}`)
	require.Equal(t, http.StatusCreated, registered.Code)
	var created map[string]string
	require.NoError(t, json.Unmarshal(registered.Body.Bytes(), &created))
	require.Equal(t, http.StatusUnauthorized, post("/api/v1/auth/login", `{"username":"bob","password":"wrong-password"}`).Code)

	// Provider down: local sign-in, refresh and token verification work.
	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"bob","password":"secret123"}`)).WithContext(ctx)
	login.Header.Set("Content-Type", "application/json")
	login.Header.Set("Origin", "http://frontend.example")
	response := httptest.NewRecorder()
	b.router.ServeHTTP(response, login)
	require.Equal(t, http.StatusOK, response.Code)
	for _, cookie := range response.Result().Cookies() {
		b.cookies[cookie.Name] = cookie
	}
	response = b.do(http.MethodPost, "/api/v1/auth/refresh")
	require.Equal(t, http.StatusOK, response.Code)
	var refreshed map[string]string
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &refreshed))
	verified, err := users.VerifyAccessToken(rpcCtx, &sso.VerifyTokenRequest{AccessToken: refreshed["access_token"]})
	require.NoError(t, err)
	require.Equal(t, "bob", verified.GetUsername())
	delete(b.cookies, "refresh_token")

	// The provider is listed as enabled but not available, and its routes answer unavailable.
	answer := providers(t, b)
	require.Len(t, answer.OIDCProviders, 1)
	require.Equal(t, "keycloak", answer.OIDCProviders[0].Name)
	require.False(t, answer.OIDCProviders[0].Available)
	response = b.get(startPath)
	require.Equal(t, http.StatusFound, response.Code)
	require.Equal(t, "http://frontend.example/auth/callback?login=error&reason=provider_unavailable", response.Header().Get("Location"))
	response = b.get(oidcCallbackPath + "?code=c&state=" + strings.Repeat("A", 43))
	require.Equal(t, http.StatusFound, response.Code)
	require.Equal(t, "/auth/callback?login=error&reason=state_invalid", response.Header().Get("Location"))

	// The provider comes up and SSO's next discovery attempt reaches it.
	idp.SetDown(false)
	require.NoError(t, provider.Discover(ctx))
	require.True(t, providers(t, b).OIDCProviders[0].Available)

	// Start redirects the browser to the provider with a state cookie.
	response = b.get(startPath)
	require.Equal(t, http.StatusFound, response.Code)
	require.Len(t, stateCookies(b), 1)
	back := idp.SignIn(response.Header().Get("Location"))
	require.Equal(t, oidcCallbackPath, back.Path)
	code, state := back.Query().Get("code"), back.Query().Get("state")
	binding := stateCookies(b)[0].Value
	require.Equal(t, "S256", idp.LastAuthorization().Get("code_challenge_method"), "the code is bound to the attempt with PKCE")

	// The callback signs the user in: a refresh cookie, no token in the URL.
	response = b.get(back.RequestURI())
	require.Equal(t, http.StatusFound, response.Code)
	require.Equal(t, "http://frontend.example/auth/callback?login=success", response.Header().Get("Location"))
	require.Empty(t, stateCookies(b))
	require.Contains(t, b.cookies, "refresh_token")

	response = b.do(http.MethodPost, "/api/v1/auth/refresh")
	require.Equal(t, http.StatusOK, response.Code)
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &refreshed))
	verified, err = users.VerifyAccessToken(rpcCtx, &sso.VerifyTokenRequest{AccessToken: refreshed["access_token"]})
	require.NoError(t, err)
	require.Equal(t, "alice", verified.GetUsername())
	require.Equal(t, "alice@example.com", verified.GetEmail())

	// Each sign-in is audited: a success under its user's scope, a failure
	// under system scope with its reason, and never what the browser sent.
	aliceID, err := uuid.Parse(verified.GetUserId())
	require.NoError(t, err)
	events := b.audit.recorded()
	require.Len(t, events, 5, "a registration, two local sign-ins and two callbacks; start and refresh are no sign-in")
	require.Equal(t, accessscope.Scope{Kind: accessscope.KindSystem}, events[0].Scope)
	require.Equal(t, [4]string{"auth.register", "success", "user", created["user_id"]}, [4]string{events[0].Event.Action, events[0].Event.Outcome, events[0].Event.ResourceType, events[0].Event.ResourceID})
	require.Equal(t, accessscope.Scope{Kind: accessscope.KindSystem}, events[1].Scope)
	require.Equal(t, [3]string{"auth.login", "failure", "invalid_credentials"}, [3]string{events[1].Event.Action, events[1].Event.Outcome, events[1].Event.ErrorCode})
	require.Equal(t, accessscope.Scope{Kind: accessscope.KindUser, UserID: bobID, Username: "bob", Role: "user"}, events[2].Scope)
	require.Equal(t, [3]string{"auth.login", "success", bobID.String()}, [3]string{events[2].Event.Action, events[2].Event.Outcome, events[2].Event.ResourceID})
	require.Equal(t, accessscope.Scope{Kind: accessscope.KindSystem}, events[3].Scope)
	require.Equal(t, [3]string{"auth.oidc_callback", "failure", "state_invalid"}, [3]string{events[3].Event.Action, events[3].Event.Outcome, events[3].Event.ErrorCode})
	require.Equal(t, accessscope.Scope{Kind: accessscope.KindUser, UserID: aliceID, Username: "alice", Role: "user"}, events[4].Scope)
	require.Equal(t, [3]string{"auth.oidc_callback", "success", aliceID.String()}, [3]string{events[4].Event.Action, events[4].Event.Outcome, events[4].Event.ResourceID})
	require.JSONEq(t, `{"provider":"keycloak"}`, events[4].Event.DetailsJSON)
	for _, event := range events {
		dump := fmt.Sprintf("%+v", event)
		for _, secret := range []string{"secret123", "wrong-password", "carol-secret", "carol@example.com", code, state, binding, refreshed["access_token"]} {
			require.NotContains(t, dump, secret)
		}
	}
}

// With OIDC off SSO lists no provider and the provider routes are not found.
func TestOIDCRoutesAreNotFoundWhenOIDCIsOff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db := dbtest.OpenSSOPostgres(t)
	svc := service.NewAuthServiceWithConfig(repository.NewUserRepository(db), jwt.NewProvider("secret"), repository.NewSessionRepository(db), service.OIDCDeps{}, service.Config{
		LocalLoginEnabled: true,
	})
	b, _ := oidcGateway(t, ctx, svc)

	require.Empty(t, providers(t, b).OIDCProviders)
	require.Equal(t, http.StatusNotFound, b.get(startPath).Code)
	require.Equal(t, http.StatusNotFound, b.get(oidcCallbackPath+"?code=c&state=s").Code)
	state := strings.Repeat("A", 43)
	b.cookies["oidc_state_x"] = &http.Cookie{Name: "oidc_state_x", Value: "binding", Path: oidcCallbackPath}
	require.Equal(t, http.StatusNotFound, b.get(oidcCallbackPath+"?code=c&state="+state).Code)
	require.Equal(t, http.StatusNotFound, b.get(oidcCallbackPath+"?error=access_denied").Code)
}

// stateCookies lists the browser-binding cookies of the attempts in flight.
func stateCookies(b *browser) []*http.Cookie {
	var cookies []*http.Cookie
	for name, cookie := range b.cookies {
		if strings.HasPrefix(name, "oidc_state_") {
			cookies = append(cookies, cookie)
		}
	}
	return cookies
}

// oidcSSO builds SSO's auth service over db with a provider that has reached
// idp, as the SSO composition root does; calling it again rebuilds the service.
// Local sign-in is on beside OIDC. Administrator groups make roles
// provider-managed.
func oidcSSO(t *testing.T, ctx context.Context, db *pgxpool.Pool, idp *oidctest.Provider, now *time.Time, adminGroups ...string) *service.AuthService {
	t.Helper()
	provider, err := oidc.NewProvider(oidc.Config{
		IssuerURL: idp.Issuer(), ClientID: idp.ClientID(), ClientSecret: idp.ClientSecret(),
		RedirectURL: "http://gateway.example" + oidcCallbackPath,
	})
	require.NoError(t, err)
	require.NoError(t, provider.Discover(ctx))
	return service.NewAuthServiceWithConfig(repository.NewUserRepository(db), jwt.NewProvider("secret"), repository.NewSessionRepository(db), service.OIDCDeps{Provider: provider, LoginAttempts: repository.NewLoginAttemptRepository(db)}, service.Config{
		LocalLoginEnabled: true, LocalRegisterEnabled: true,
		OIDCEnabled: true, OIDCProviderName: "keycloak", OIDCAdminGroups: adminGroups, Now: func() time.Time { return *now },
	})
}

// callback replays a provider's redirect back to Gateway with the given
// binding cookie, as a browser holding that cookie would.
func (b *browser) callback(back *url.URL, cookieName, binding string) *httptest.ResponseRecorder {
	b.t.Helper()
	b.cookies[cookieName] = &http.Cookie{Name: cookieName, Value: binding, Path: oidcCallbackPath}
	return b.get(back.RequestURI())
}

// Login attempts live in PostgreSQL: an attempt started before SSO is rebuilt
// completes afterwards, two attempts in one browser complete independently, a
// callback with another attempt's binding consumes nothing, and an attempt is
// usable once and for 30 minutes.
func TestOIDCLoginAttemptsAreDurableSingleUseAndPerBrowserTab(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := dbtest.OpenSSOPostgres(t)
	idp := oidctest.New(t, oidctest.Options{RedirectURL: "http://gateway.example" + oidcCallbackPath})
	now := time.Now()
	before, _ := oidcGateway(t, ctx, oidcSSO(t, ctx, db, idp, &now))

	// Two tabs start sign-in; each attempt gets a cookie of its own.
	start := func() (*url.URL, *http.Cookie) {
		t.Helper()
		known := map[string]bool{}
		for _, cookie := range stateCookies(before) {
			known[cookie.Name] = true
		}
		response := before.get(startPath)
		require.Equal(t, http.StatusFound, response.Code)
		for _, cookie := range stateCookies(before) {
			if !known[cookie.Name] {
				require.InDelta(t, 30*60, cookie.MaxAge, 2, "the cookie lives as long as the attempt")
				return idp.SignIn(response.Header().Get("Location")), cookie
			}
		}
		t.Fatal("start set no new binding cookie")
		return nil, nil
	}
	firstBack, first := start()
	secondBack, second := start()
	require.NotEqual(t, first.Name, second.Name)
	require.Len(t, stateCookies(before), 2)
	firstCookie, firstBinding := first.Name, first.Value
	secondCookie, secondBinding := second.Name, second.Value

	// SSO is rebuilt; the attempts are still in PostgreSQL.
	after, users := oidcGateway(t, ctx, oidcSSO(t, ctx, db, idp, &now))
	rpcCtx := accessscope.WithSystemScope(ctx)

	// The first attempt's callback with the second attempt's binding is refused
	// and consumes neither attempt.
	response := after.callback(firstBack, firstCookie, secondBinding)
	require.Equal(t, http.StatusFound, response.Code)
	require.Equal(t, "/auth/callback?login=error&reason=state_invalid", response.Header().Get("Location"))
	require.NotContains(t, after.cookies, firstCookie, "the cookie is cleared on a refused callback")

	for _, attempt := range []struct {
		back            *url.URL
		cookie, binding string
	}{{secondBack, secondCookie, secondBinding}, {firstBack, firstCookie, firstBinding}} {
		delete(after.cookies, "refresh_token")
		response = after.callback(attempt.back, attempt.cookie, attempt.binding)
		require.Equal(t, http.StatusFound, response.Code)
		require.Equal(t, "http://frontend.example/auth/callback?login=success", response.Header().Get("Location"))
		require.NotContains(t, after.cookies, attempt.cookie)
		response = after.do(http.MethodPost, "/api/v1/auth/refresh")
		require.Equal(t, http.StatusOK, response.Code)
		var refreshed map[string]string
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &refreshed))
		verified, err := users.VerifyAccessToken(rpcCtx, &sso.VerifyTokenRequest{AccessToken: refreshed["access_token"]})
		require.NoError(t, err)
		require.Equal(t, "alice", verified.GetUsername())
	}

	// A replayed callback finds its attempt used.
	delete(after.cookies, "refresh_token")
	response = after.callback(firstBack, firstCookie, firstBinding)
	require.Equal(t, http.StatusFound, response.Code)
	require.Equal(t, "/auth/callback?login=error&reason=state_invalid", response.Header().Get("Location"))
	require.NotContains(t, after.cookies, "refresh_token")

	// An attempt returned to after 30 minutes has expired, cleanup or not.
	before.cookies = map[string]*http.Cookie{}
	lateBack, late := start()
	now = now.Add(30 * time.Minute)
	response = after.callback(lateBack, late.Name, late.Value)
	require.Equal(t, http.StatusFound, response.Code)
	require.Equal(t, "/auth/callback?login=error&reason=state_invalid", response.Header().Get("Location"))

	// Cancelling at the provider lands on the login page with a reason.
	response = after.get(oidcCallbackPath + "?error=access_denied&state=" + lateBack.Query().Get("state"))
	require.Equal(t, http.StatusFound, response.Code)
	require.Equal(t, "/auth/callback?login=error&reason=access_denied", response.Header().Get("Location"))
}

// loginTokens returns only the tokens of a sign-in.
func loginTokens(signIn model.SignIn, err error) (model.Tokens, error) {
	return signIn.Tokens, err
}
