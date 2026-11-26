package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/auditlog"
	gatewayhandlermocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/gateway/http/handler"
)

const (
	loginPage       = "http://frontend.example/login"
	backchannelPath = "/api/v1/auth/oidc/keycloak/backchannel-logout"
	logoutToken     = "logout-token-value"
)

// Signing out of a session created through OIDC answers the provider sign-out
// URL SSO built for the validated request origin's login page; a local session
// gets an empty object, as before.
func TestAuthHandlerLogoutAnswersTheProviderSignOutURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := gatewayhandlermocks.NewMockAuthService(t)
	h := handler.NewAuthHandler(auth, anyAudit(t), handler.CookieConfig{Name: "refresh_token"}, testRedirectConfig())
	router := gin.New()
	router.POST("/logout", h.Logout)
	auth.EXPECT().Logout(mock.Anything, "oidc-refresh", loginPage).Return("https://idp.example/logout?id_token_hint=x", nil)
	auth.EXPECT().Logout(mock.Anything, "local-refresh", loginPage).Return("", nil)

	resp := perform(router, http.MethodPost, "/logout", "", []*http.Cookie{{Name: "refresh_token", Value: "oidc-refresh"}})
	require.Equal(t, http.StatusOK, resp.Code)
	require.JSONEq(t, `{"provider_logout_url":"https://idp.example/logout?id_token_hint=x"}`, resp.Body.String())
	require.Equal(t, "no-store", resp.Header().Get("Cache-Control"))
	requireCookieMaxAge(t, resp.Result(), "refresh_token", -1)

	resp = perform(router, http.MethodPost, "/logout", "", []*http.Cookie{{Name: "refresh_token", Value: "local-refresh"}})
	require.Equal(t, http.StatusOK, resp.Code)
	require.JSONEq(t, `{}`, resp.Body.String())
	requireCookieMaxAge(t, resp.Result(), "refresh_token", -1)
}

func backchannelRouter(t *testing.T) (*gatewayhandlermocks.MockAuthService, *auditLog, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	auth := gatewayhandlermocks.NewMockAuthService(t)
	recorder, audit := expectAudit(t, nil)
	h := handler.NewAuthHandler(auth, recorder, handler.CookieConfig{Name: "refresh_token"}, testRedirectConfig())
	router := gin.New()
	router.POST(backchannelPath, h.KeycloakBackchannelLogout)
	return auth, audit, router
}

// postLogoutToken posts as the provider does: a form body, no cookie and no
// browser origin.
func postLogoutToken(router http.Handler, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, backchannelPath, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

// The back-channel route answers 200 for a valid token whether or not it
// matched a session, 400 for a refused one, 503 while the provider's keys are
// unavailable and 404 with OIDC off, always uncached.
func TestAuthHandlerBackchannelLogoutAnswers(t *testing.T) {
	for _, test := range []struct {
		name    string
		revoked int
		err     error
		want    int
	}{
		{name: "revoked sessions", revoked: 2, want: http.StatusOK},
		{name: "nothing matched", want: http.StatusOK},
		{name: "refused token", err: apperrors.New(apperrors.ErrBadRequest, "invalid logout token"), want: http.StatusBadRequest},
		{name: "keys unavailable", err: apperrors.ErrUnavailable, want: http.StatusServiceUnavailable},
		{name: "provider timed out", err: apperrors.ErrTimeout, want: http.StatusServiceUnavailable},
		{name: "oidc off", err: apperrors.ErrNotFound, want: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			auth, _, router := backchannelRouter(t)
			auth.EXPECT().BackchannelLogout(mock.Anything, "keycloak", logoutToken).Return(test.revoked, test.err)

			resp := postLogoutToken(router, "application/x-www-form-urlencoded", url.Values{"logout_token": {logoutToken}}.Encode())

			require.Equal(t, test.want, resp.Code)
			require.Equal(t, "no-store", resp.Header().Get("Cache-Control"))
			require.NotContains(t, resp.Body.String(), logoutToken)
		})
	}
}

// A request without a form token, or with a body over the bound, reaches SSO
// with no token, so with OIDC off it is still not found and otherwise refused.
func TestAuthHandlerBackchannelLogoutSendsSSONoTokenItCannotRead(t *testing.T) {
	for name, request := range map[string]struct{ contentType, body string }{
		"json body":     {"application/json", `{"logout_token":"` + logoutToken + `"}`},
		"no token":      {"application/x-www-form-urlencoded", "other=value"},
		"oversized":     {"application/x-www-form-urlencoded", "logout_token=" + logoutToken + "&padding=" + strings.Repeat("a", 70<<10)},
		"token in body": {"text/plain", "logout_token=" + logoutToken},
	} {
		t.Run(name, func(t *testing.T) {
			auth, _, router := backchannelRouter(t)
			auth.EXPECT().BackchannelLogout(mock.Anything, "keycloak", "").Return(0, apperrors.New(apperrors.ErrBadRequest, "invalid logout token"))

			resp := postLogoutToken(router, request.contentType, request.body)

			require.Equal(t, http.StatusBadRequest, resp.Code)
		})
	}
}

// A verified back-channel logout is audited under system scope with the
// provider and the number of sessions it revoked; a refused one is not, and
// no event holds the token.
func TestAuthHandlerRecordsAVerifiedBackchannelLogout(t *testing.T) {
	auth, audit, router := backchannelRouter(t)
	auth.EXPECT().BackchannelLogout(mock.Anything, "keycloak", logoutToken).Return(2, nil).Once()
	auth.EXPECT().BackchannelLogout(mock.Anything, "keycloak", "refused").Return(0, apperrors.New(apperrors.ErrBadRequest, "invalid logout token")).Once()
	auth.EXPECT().BackchannelLogout(mock.Anything, "keycloak", "keys-down").Return(0, apperrors.ErrUnavailable).Once()

	for _, token := range []string{logoutToken, "refused", "keys-down"} {
		postLogoutToken(router, "application/x-www-form-urlencoded", url.Values{"logout_token": {token}}.Encode())
	}

	event := audit.only(t)
	require.Equal(t, accessscope.Scope{Kind: accessscope.KindSystem}, event.Scope)
	require.Equal(t, auditlog.ActionAuthProviderLogout, event.Event.Action)
	require.Equal(t, auditlog.ResourceAuth, event.Event.ResourceType)
	require.Equal(t, auditlog.OutcomeSuccess, event.Event.Outcome)
	var details map[string]string
	require.NoError(t, json.Unmarshal([]byte(event.Event.DetailsJSON), &details))
	require.Equal(t, map[string]string{"provider": "keycloak", "revoked_sessions": "2"}, details)
	requireNoCredentials(t, audit.recorded(), logoutToken)
}
