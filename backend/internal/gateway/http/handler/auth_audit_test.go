package handler_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/auditlog"
	gatewayhandlermocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/gateway/http/handler"
)

// auditedEvent is an event as Core receives it: the payload and the scope the
// call carried, which is where Core takes the actor from.
type auditedEvent struct {
	Scope accessscope.Scope
	Event model.AuditEvent
}

// auditLog holds the events a handler sent to the audit mock.
type auditLog struct {
	mu     sync.Mutex
	events []auditedEvent
}

func (l *auditLog) recorded() []auditedEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]auditedEvent(nil), l.events...)
}

// expectAudit returns an audit mock that accepts any number of events,
// answering err, and the log of what it received.
func expectAudit(t *testing.T, err error) (*gatewayhandlermocks.MockAuditRecorder, *auditLog) {
	t.Helper()
	received := &auditLog{}
	recorder := gatewayhandlermocks.NewMockAuditRecorder(t)
	recorder.EXPECT().RecordAuditEvent(mock.Anything, mock.Anything).Run(func(ctx context.Context, event model.AuditEvent) {
		scope, _ := accessscope.FromContext(ctx)
		received.mu.Lock()
		defer received.mu.Unlock()
		received.events = append(received.events, auditedEvent{Scope: scope, Event: event})
	}).Return(err).Maybe()
	return recorder, received
}

func (l *auditLog) only(t *testing.T) auditedEvent {
	t.Helper()
	events := l.recorded()
	require.Len(t, events, 1)
	return events[0]
}

// Values a sign-in carries that no audit event may hold.
const (
	attemptedUsername = "mallory"
	attemptedPassword = "hunter2-secret"
	authCode          = "authorization-code-value"
	stateBinding      = "state-binding-value"
	accessToken       = "access-token-value"
	refreshToken      = "refresh-token-value"
)

func requireNoCredentials(t *testing.T, events []auditedEvent, secrets ...string) {
	t.Helper()
	for _, event := range events {
		dump := fmt.Sprintf("%+v", event)
		for _, secret := range secrets {
			require.NotContains(t, dump, secret)
		}
	}
}

func auditedAuthRouter(t *testing.T, audit handler.AuditRecorder) (*gatewayhandlermocks.MockAuthService, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	auth := gatewayhandlermocks.NewMockAuthService(t)
	h := handler.NewAuthHandler(auth, audit, handler.CookieConfig{Name: "refresh_token", Path: "/", HTTPOnly: true}, testRedirectConfig())
	router := gin.New()
	router.POST("/register", h.Register)
	router.POST("/login", h.Login)
	router.GET("/callback", h.CompleteKeycloakCallback)
	return auth, router
}

func liveTokens() model.Tokens {
	return model.Tokens{AccessToken: accessToken, RefreshToken: refreshToken, RefreshExpiresAt: time.Now().Add(time.Hour)}
}

func TestAuthHandlerRecordsLocalSignIns(t *testing.T) {
	userID := uuid.New()
	for name, tc := range map[string]struct {
		role string
		kind accessscope.Kind
	}{
		"a user":           {role: "user", kind: accessscope.KindUser},
		"an administrator": {role: "admin", kind: accessscope.KindAdmin},
	} {
		t.Run("success of "+name, func(t *testing.T) {
			recorder, audit := expectAudit(t, nil)
			auth, router := auditedAuthRouter(t, recorder)
			auth.EXPECT().Login(mock.Anything, "alice", attemptedPassword).
				Return(model.SignIn{Tokens: liveTokens(), User: model.SignedInUser{ID: userID.String(), Username: "alice", Role: tc.role}}, nil)

			resp := perform(router, http.MethodPost, "/login", `{"username":"alice","password":"`+attemptedPassword+`"}`, nil)
			require.Equal(t, http.StatusOK, resp.Code)

			got := audit.only(t)
			require.Equal(t, accessscope.Scope{Kind: tc.kind, UserID: userID, Username: "alice", Role: tc.role}, got.Scope)
			require.Equal(t, auditlog.ActionAuthLogin, got.Event.Action)
			require.Equal(t, auditlog.OutcomeSuccess, got.Event.Outcome)
			require.Equal(t, auditlog.ResourceAuth, got.Event.ResourceType)
			require.Equal(t, userID.String(), got.Event.ResourceID)
			require.Equal(t, userID.String(), got.Event.OwnerID)
			require.Empty(t, got.Event.ErrorCode)
			requireNoCredentials(t, audit.recorded(), attemptedPassword, accessToken, refreshToken)
		})
	}

	for name, tc := range map[string]struct {
		body string
		sso  error
		code string
	}{
		"wrong credentials":     {body: `{"username":"` + attemptedUsername + `","password":"` + attemptedPassword + `"}`, sso: apperrors.ErrInvalidCredentials, code: "invalid_credentials"},
		"SSO unavailable":       {body: `{"username":"` + attemptedUsername + `","password":"` + attemptedPassword + `"}`, sso: apperrors.ErrUnavailable, code: "unavailable"},
		"local sign-in off":     {body: `{"username":"` + attemptedUsername + `","password":"` + attemptedPassword + `"}`, sso: apperrors.ErrNotFound, code: "not_found"},
		"a malformed username":  {body: `{"username":"bad name ` + attemptedUsername + `","password":"` + attemptedPassword + `"}`, code: "bad_request"},
		"a malformed JSON body": {body: `{"username":"` + attemptedUsername, code: "bad_request"},
	} {
		t.Run("failure on "+name, func(t *testing.T) {
			recorder, audit := expectAudit(t, nil)
			auth, router := auditedAuthRouter(t, recorder)
			if tc.sso != nil {
				auth.EXPECT().Login(mock.Anything, attemptedUsername, attemptedPassword).Return(model.SignIn{}, tc.sso)
			}

			perform(router, http.MethodPost, "/login", tc.body, nil)

			got := audit.only(t)
			require.Equal(t, accessscope.Scope{Kind: accessscope.KindSystem}, got.Scope)
			require.Equal(t, auditlog.ActionAuthLogin, got.Event.Action)
			require.Equal(t, auditlog.OutcomeFailure, got.Event.Outcome)
			require.Equal(t, auditlog.ResourceAuth, got.Event.ResourceType)
			require.Equal(t, tc.code, got.Event.ErrorCode)
			require.Empty(t, got.Event.ActorUserID)
			require.Empty(t, got.Event.ActorUsername)
			requireNoCredentials(t, audit.recorded(), attemptedUsername, attemptedPassword)
		})
	}
}

func TestAuthHandlerRecordsOIDCCallbacks(t *testing.T) {
	state, cookieName := testState("audited")
	withCookie := []*http.Cookie{{Name: cookieName, Value: stateBinding}}
	callback := "/callback?code=" + authCode + "&state=" + state

	t.Run("success", func(t *testing.T) {
		userID := uuid.New()
		recorder, audit := expectAudit(t, nil)
		auth, router := auditedAuthRouter(t, recorder)
		auth.EXPECT().CompleteOIDCCallback(mock.Anything, "keycloak", authCode, state, stateBinding).Return(model.OIDCCallbackResult{
			Outcome: model.OIDCCallbackSuccess,
			Tokens:  liveTokens(),
			User:    model.SignedInUser{ID: userID.String(), Username: "bob", Role: "admin"},
		}, nil)

		resp := perform(router, http.MethodGet, callback, ``, withCookie)
		requireLoginRedirect(t, resp, "/auth/callback?login=success")

		got := audit.only(t)
		require.Equal(t, accessscope.Scope{Kind: accessscope.KindAdmin, UserID: userID, Username: "bob", Role: "admin"}, got.Scope)
		require.Equal(t, auditlog.ActionAuthOIDCCallback, got.Event.Action)
		require.Equal(t, auditlog.OutcomeSuccess, got.Event.Outcome)
		require.Equal(t, auditlog.ResourceAuth, got.Event.ResourceType)
		require.Equal(t, userID.String(), got.Event.ResourceID)
		require.Equal(t, userID.String(), got.Event.OwnerID)
		require.JSONEq(t, `{"provider":"keycloak"}`, got.Event.DetailsJSON)
		requireNoCredentials(t, audit.recorded(), authCode, state, stateBinding, accessToken, refreshToken)
	})

	outcome := func(outcome model.OIDCCallbackOutcome) func(*gatewayhandlermocks.MockAuthService) {
		return func(auth *gatewayhandlermocks.MockAuthService) {
			auth.EXPECT().CompleteOIDCCallback(mock.Anything, "keycloak", authCode, state, stateBinding).Return(model.OIDCCallbackResult{Outcome: outcome}, nil)
		}
	}
	for name, tc := range map[string]struct {
		query   string
		cookies []*http.Cookie
		sso     func(*gatewayhandlermocks.MockAuthService)
		reason  string
	}{
		"cancelled at the provider": {query: "/callback?error=access_denied&state=" + state, cookies: withCookie, reason: "access_denied"},
		"a missing binding cookie":  {query: callback, reason: "state_invalid"},
		"a used attempt":            {query: callback, cookies: withCookie, sso: outcome(model.OIDCCallbackInvalidAttempt), reason: "state_invalid"},
		"a rejected identity":       {query: callback, cookies: withCookie, sso: outcome(model.OIDCCallbackIdentityRejected), reason: "identity_rejected"},
		"a deactivated account":     {query: callback, cookies: withCookie, sso: outcome(model.OIDCCallbackAccountDeactivated), reason: "account_deactivated"},
		"the provider unavailable": {query: callback, cookies: withCookie, sso: func(auth *gatewayhandlermocks.MockAuthService) {
			auth.EXPECT().CompleteOIDCCallback(mock.Anything, "keycloak", authCode, state, stateBinding).Return(model.OIDCCallbackResult{}, apperrors.ErrUnavailable)
		}, reason: "provider_unavailable"},
		"a success without usable tokens": {query: callback, cookies: withCookie, sso: func(auth *gatewayhandlermocks.MockAuthService) {
			auth.EXPECT().CompleteOIDCCallback(mock.Anything, "keycloak", authCode, state, stateBinding).Return(model.OIDCCallbackResult{
				Outcome: model.OIDCCallbackSuccess, User: model.SignedInUser{ID: uuid.NewString(), Username: "bob", Role: "user"},
			}, nil)
		}, reason: "login_failed"},
	} {
		t.Run("failure on "+name, func(t *testing.T) {
			recorder, audit := expectAudit(t, nil)
			auth, router := auditedAuthRouter(t, recorder)
			expectKeycloakEnabled(auth)
			if tc.sso != nil {
				tc.sso(auth)
			}

			perform(router, http.MethodGet, tc.query, ``, tc.cookies)

			got := audit.only(t)
			require.Equal(t, accessscope.Scope{Kind: accessscope.KindSystem}, got.Scope)
			require.Equal(t, auditlog.ActionAuthOIDCCallback, got.Event.Action)
			require.Equal(t, auditlog.OutcomeFailure, got.Event.Outcome)
			require.Equal(t, tc.reason, got.Event.ErrorCode)
			require.Empty(t, got.Event.ResourceID)
			require.JSONEq(t, `{"provider":"keycloak"}`, got.Event.DetailsJSON)
			requireNoCredentials(t, audit.recorded(), authCode, state, stateBinding, accessToken, refreshToken)
		})
	}

	t.Run("nothing with OIDC off", func(t *testing.T) {
		// The mock expects no call, so any audit write fails the test.
		auth, router := auditedAuthRouter(t, gatewayhandlermocks.NewMockAuditRecorder(t))
		auth.EXPECT().CompleteOIDCCallback(mock.Anything, "keycloak", authCode, state, stateBinding).Return(model.OIDCCallbackResult{}, apperrors.ErrNotFound)

		resp := perform(router, http.MethodGet, callback, ``, withCookie)
		require.Equal(t, http.StatusNotFound, resp.Code)
	})
}

func TestAuthHandlerRecordsRegistrations(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		userID := uuid.New()
		recorder, audit := expectAudit(t, nil)
		auth, router := auditedAuthRouter(t, recorder)
		auth.EXPECT().Register(mock.Anything, attemptedUsername, "m@example.com", attemptedPassword).Return(userID.String(), nil)

		resp := perform(router, http.MethodPost, "/register", `{"username":"`+attemptedUsername+`","email":"m@example.com","password":"`+attemptedPassword+`"}`, nil)
		require.Equal(t, http.StatusCreated, resp.Code)

		got := audit.only(t)
		require.Equal(t, accessscope.Scope{Kind: accessscope.KindSystem}, got.Scope)
		require.Equal(t, auditlog.ActionAuthRegister, got.Event.Action)
		require.Equal(t, auditlog.OutcomeSuccess, got.Event.Outcome)
		require.Equal(t, auditlog.ResourceUser, got.Event.ResourceType)
		require.Equal(t, userID.String(), got.Event.ResourceID)
		require.Equal(t, userID.String(), got.Event.OwnerID)
		requireNoCredentials(t, audit.recorded(), attemptedUsername, attemptedPassword, "m@example.com")
	})

	t.Run("failure", func(t *testing.T) {
		recorder, audit := expectAudit(t, nil)
		auth, router := auditedAuthRouter(t, recorder)
		auth.EXPECT().Register(mock.Anything, attemptedUsername, "m@example.com", attemptedPassword).Return("", apperrors.ErrAlreadyExists)

		resp := perform(router, http.MethodPost, "/register", `{"username":"`+attemptedUsername+`","email":"m@example.com","password":"`+attemptedPassword+`"}`, nil)
		require.Equal(t, http.StatusConflict, resp.Code)

		got := audit.only(t)
		require.Equal(t, accessscope.Scope{Kind: accessscope.KindSystem}, got.Scope)
		require.Equal(t, auditlog.ActionAuthRegister, got.Event.Action)
		require.Equal(t, auditlog.OutcomeFailure, got.Event.Outcome)
		require.Equal(t, auditlog.ResourceUser, got.Event.ResourceType)
		require.Equal(t, "already_exists", got.Event.ErrorCode)
		require.Empty(t, got.Event.ResourceID)
		requireNoCredentials(t, audit.recorded(), attemptedUsername, attemptedPassword, "m@example.com")
	})
}

// A failed audit write answers exactly as a successful one: same status,
// body, redirect and cookies.
func TestAuthHandlerAnswersTheSameWhenTheAuditWriteFails(t *testing.T) {
	state, cookieName := testState("audit-failure")
	userID := uuid.NewString()
	user := model.SignedInUser{ID: userID, Username: "alice", Role: "user"}
	for name, tc := range map[string]struct {
		method, path, body string
		cookies            []*http.Cookie
		sso                func(*gatewayhandlermocks.MockAuthService, model.Tokens)
	}{
		"a sign-in": {method: http.MethodPost, path: "/login", body: `{"username":"alice","password":"pw"}`, sso: func(auth *gatewayhandlermocks.MockAuthService, tokens model.Tokens) {
			auth.EXPECT().Login(mock.Anything, "alice", "pw").Return(model.SignIn{Tokens: tokens, User: user}, nil)
		}},
		"a failed sign-in": {method: http.MethodPost, path: "/login", body: `{"username":"alice","password":"pw"}`, sso: func(auth *gatewayhandlermocks.MockAuthService, _ model.Tokens) {
			auth.EXPECT().Login(mock.Anything, "alice", "pw").Return(model.SignIn{}, apperrors.ErrInvalidCredentials)
		}},
		"a callback": {method: http.MethodGet, path: "/callback?code=c&state=" + state, cookies: []*http.Cookie{{Name: cookieName, Value: "b"}}, sso: func(auth *gatewayhandlermocks.MockAuthService, tokens model.Tokens) {
			auth.EXPECT().CompleteOIDCCallback(mock.Anything, "keycloak", "c", state, "b").Return(model.OIDCCallbackResult{Outcome: model.OIDCCallbackSuccess, Tokens: tokens, User: user}, nil)
		}},
		"a failed callback": {method: http.MethodGet, path: "/callback?code=c&state=" + state, cookies: []*http.Cookie{{Name: cookieName, Value: "b"}}, sso: func(auth *gatewayhandlermocks.MockAuthService, _ model.Tokens) {
			auth.EXPECT().CompleteOIDCCallback(mock.Anything, "keycloak", "c", state, "b").Return(model.OIDCCallbackResult{Outcome: model.OIDCCallbackIdentityRejected}, nil)
		}},
		"a registration": {method: http.MethodPost, path: "/register", body: `{"username":"alice","email":"a@example.com","password":"long-enough-pw"}`, sso: func(auth *gatewayhandlermocks.MockAuthService, _ model.Tokens) {
			auth.EXPECT().Register(mock.Anything, "alice", "a@example.com", "long-enough-pw").Return(userID, nil)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			tokens := model.Tokens{AccessToken: "a", RefreshToken: "r", RefreshExpiresAt: time.Now().Add(time.Hour)}
			answer := func(recorder handler.AuditRecorder) *httptest.ResponseRecorder {
				auth, router := auditedAuthRouter(t, recorder)
				tc.sso(auth, tokens)
				return perform(router, tc.method, tc.path, tc.body, tc.cookies)
			}
			working, _ := expectAudit(t, nil)
			recorded := answer(working)
			broken, failing := expectAudit(t, errors.New("clickhouse down"))
			resp := answer(broken)

			require.Len(t, failing.recorded(), 1, "the write was attempted")
			require.Equal(t, recorded.Code, resp.Code)
			require.Equal(t, recorded.Body.String(), resp.Body.String())
			require.Equal(t, recorded.Header().Get("Location"), resp.Header().Get("Location"))
			require.Equal(t, cookieValues(recorded), cookieValues(resp))
		})
	}
}

// cookieValues names each cookie a response set with its value, or "cleared".
func cookieValues(resp *httptest.ResponseRecorder) map[string]string {
	values := map[string]string{}
	for _, cookie := range resp.Result().Cookies() {
		if cookie.MaxAge < 0 {
			values[cookie.Name] = "cleared"
			continue
		}
		values[cookie.Name] = cookie.Value
	}
	return values
}
