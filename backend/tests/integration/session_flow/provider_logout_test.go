package session_flow_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/oidctest"
)

const backchannelLogoutPath = "/api/v1/auth/oidc/keycloak/backchannel-logout"

// tab is another browser against the same Gateway, with its own cookies.
func (b *browser) tab() *browser {
	return &browser{t: b.t, ctx: b.ctx, router: b.router, cookies: map[string]*http.Cookie{}, audit: b.audit}
}

// signInAt signs identity in through Gateway and the provider and returns the
// provider session (sid) of that sign-in.
func (b *browser) signInAt(idp *oidctest.Provider, identity oidctest.Identity) string {
	b.t.Helper()
	idp.SetIdentity(identity)
	response := b.get(startPath)
	require.Equal(b.t, http.StatusFound, response.Code)
	back := idp.SignIn(response.Header().Get("Location"))
	response = b.get(back.RequestURI())
	require.Equal(b.t, "http://frontend.example/auth/callback?login=success", response.Header().Get("Location"))
	return idp.LastSessionID()
}

// refreshes reports whether the browser's session still refreshes.
func (b *browser) refreshes() bool {
	b.t.Helper()
	response := b.do(http.MethodPost, "/api/v1/auth/refresh")
	require.Contains(b.t, []int{http.StatusOK, http.StatusUnauthorized}, response.Code)
	return response.Code == http.StatusOK
}

// postBackchannelLogout posts token as the provider's server does: a form
// body, no cookie and no browser origin.
func postBackchannelLogout(b *browser, token string) *httptest.ResponseRecorder {
	b.t.Helper()
	req := httptest.NewRequest(http.MethodPost, backchannelLogoutPath, strings.NewReader(url.Values{"logout_token": {token}}.Encode())).WithContext(b.ctx)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	b.router.ServeHTTP(response, req)
	require.Equal(b.t, "no-store", response.Header().Get("Cache-Control"))
	return response
}

// Changes at Keycloak reach DCM: a back-channel logout naming a provider
// session ends the DCM sessions signed in with it, one naming only a subject
// ends every session of that user, and other users keep working. An OIDC
// session ends 12 hours after sign-in. Signing out of DCM answers the
// provider's sign-out URL for an OIDC session and none for a local one, and
// the back-channel call Keycloak then makes succeeds without effect.
func TestProviderDrivenSignOutThroughGateway(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := dbtest.OpenSSOPostgres(t)
	idp := oidctest.New(t, oidctest.Options{RedirectURL: "http://gateway.example" + oidcCallbackPath})
	now := time.Now()
	svc := oidcSSO(t, ctx, db, idp, &now)
	_, err := svc.Register(ctx, "carol", "carol@example.com", "secret123")
	require.NoError(t, err)
	laptop, _ := oidcGateway(t, ctx, svc)
	phone, bob, carol := laptop.tab(), laptop.tab(), laptop.tab()
	alice := oidctest.Identity{Subject: "subject-alice", PreferredUsername: "alice", Email: "alice@example.com", EmailVerified: true}
	laptopSession := laptop.signInAt(idp, alice)
	phone.signInAt(idp, alice)
	bob.signInAt(idp, oidctest.Identity{Subject: "subject-bob", PreferredUsername: "bob"})
	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"carol","password":"secret123"}`)).WithContext(ctx)
	login.Header.Set("Content-Type", "application/json")
	recorded := httptest.NewRecorder()
	laptop.router.ServeHTTP(recorded, login)
	require.Equal(t, http.StatusOK, recorded.Code)
	for _, cookie := range recorded.Result().Cookies() {
		carol.cookies[cookie.Name] = cookie
	}

	// Signing out in Keycloak on the laptop ends only the laptop's session.
	require.Equal(t, http.StatusOK, postBackchannelLogout(laptop, idp.LogoutToken("subject-alice", laptopSession, nil)).Code)
	require.False(t, laptop.refreshes())
	require.True(t, phone.refreshes())
	require.True(t, bob.refreshes())

	// Keycloak ending every session of the user ends the phone's too.
	require.Equal(t, http.StatusOK, postBackchannelLogout(laptop, idp.LogoutToken("subject-alice", "", nil)).Code)
	require.False(t, phone.refreshes())
	require.True(t, bob.refreshes())
	require.True(t, carol.refreshes())

	// A token the checks refuse revokes nothing.
	response := postBackchannelLogout(laptop, idp.LogoutToken("subject-bob", "", func(c map[string]any) { c["nonce"] = "n" }))
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.True(t, bob.refreshes())

	// Signing out of DCM from an OIDC session sends the browser on to Keycloak.
	dave := laptop.tab()
	daveSession := dave.signInAt(idp, oidctest.Identity{Subject: "subject-dave", PreferredUsername: "dave"})
	daveIDToken := idp.LastIDToken()
	response = dave.do(http.MethodPost, "/api/v1/auth/logout")
	require.Equal(t, http.StatusOK, response.Code)
	var signedOut map[string]string
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &signedOut))
	signOut, err := url.Parse(signedOut["provider_logout_url"])
	require.NoError(t, err)
	require.Equal(t, idp.Issuer()+oidctest.PathEndSession, signOut.Scheme+"://"+signOut.Host+signOut.Path)
	require.Equal(t, url.Values{
		"id_token_hint": {daveIDToken}, "client_id": {idp.ClientID()}, "post_logout_redirect_uri": {"http://frontend.example/login"},
	}, signOut.Query())
	require.False(t, dave.refreshes())
	// Keycloak then calls back for the session DCM already ended.
	require.Equal(t, http.StatusOK, postBackchannelLogout(laptop, idp.LogoutToken("subject-dave", daveSession, nil)).Code)

	// A local session signs out as before, with no provider URL.
	response = carol.do(http.MethodPost, "/api/v1/auth/logout")
	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `{}`, response.Body.String())

	// Twelve hours after sign-in an OIDC session no longer refreshes.
	now = now.Add(12 * time.Hour)
	require.False(t, bob.refreshes())

	// Each verified back-channel logout is audited under system scope with
	// the number of sessions it ended, and no event holds a token.
	var revoked []string
	for _, event := range laptop.audit.recorded() {
		if event.Event.Action != "auth.provider_logout" {
			continue
		}
		require.Equal(t, accessscope.Scope{Kind: accessscope.KindSystem}, event.Scope)
		require.Equal(t, "success", event.Event.Outcome)
		var details map[string]string
		require.NoError(t, json.Unmarshal([]byte(event.Event.DetailsJSON), &details))
		require.Equal(t, "keycloak", details["provider"])
		revoked = append(revoked, details["revoked_sessions"])
	}
	require.Equal(t, []string{"1", "1", "0"}, revoked)
	for _, event := range laptop.audit.recorded() {
		require.NotContains(t, fmt.Sprintf("%+v", event), daveIDToken)
	}
}
