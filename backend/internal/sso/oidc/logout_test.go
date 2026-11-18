package oidc_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/oidc"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/oidctest"
)

func TestExchangeCodeReturnsTheProviderSessionAndTheIDToken(t *testing.T) {
	idp := oidctest.New(t, oidctest.Options{})
	idp.SetIdentity(oidctest.Identity{Subject: "subject-1", PreferredUsername: "alice", SessionID: "provider-session-1"})
	provider := discovered(t, idp)

	identity, err := provider.ExchangeCode(context.Background(), signInCode(t, idp, provider), testNonce, testVerifier)

	require.NoError(t, err)
	require.Equal(t, "provider-session-1", identity.SessionID)
	require.Equal(t, idp.LastIDToken(), identity.IDToken)
	require.NotEmpty(t, identity.IDToken)
}

func TestEndSessionURLCarriesTheHintTheClientAndThePostLogoutRedirect(t *testing.T) {
	idp := oidctest.New(t, oidctest.Options{})
	provider := discovered(t, idp)

	parsed, err := url.Parse(provider.EndSessionURL("id-token-value", "http://frontend.example/login"))

	require.NoError(t, err)
	require.Equal(t, idp.Issuer()+oidctest.PathEndSession, parsed.Scheme+"://"+parsed.Host+parsed.Path)
	require.Equal(t, url.Values{
		"id_token_hint":            {"id-token-value"},
		"client_id":                {idp.ClientID()},
		"post_logout_redirect_uri": {"http://frontend.example/login"},
	}, parsed.Query())

	parsed, err = url.Parse(provider.EndSessionURL("id-token-value", ""))
	require.NoError(t, err)
	require.False(t, parsed.Query().Has("post_logout_redirect_uri"))
}

func TestEndSessionURLIsEmptyWithoutAnAdvertisedEndpointOrBeforeDiscovery(t *testing.T) {
	without := discovered(t, oidctest.New(t, oidctest.Options{NoEndSession: true}))
	require.Empty(t, without.EndSessionURL("id-token-value", "http://frontend.example/login"))

	undiscovered, err := oidc.NewProvider(fakeProviderConfig(oidctest.New(t, oidctest.Options{})))
	require.NoError(t, err)
	require.Empty(t, undiscovered.EndSessionURL("id-token-value", "http://frontend.example/login"))
}

func TestLogoutTokenNamesTheProviderSessionOrTheSubject(t *testing.T) {
	idp := oidctest.New(t, oidctest.Options{})
	provider := discovered(t, idp)
	for _, want := range []service.OIDCLogout{
		{Subject: "subject-1", SessionID: "provider-session-1"},
		{Subject: "subject-1"},
		{SessionID: "provider-session-1"},
	} {
		got, err := provider.VerifyLogoutToken(context.Background(), idp.LogoutToken(want.Subject, want.SessionID, nil))
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}

func TestLogoutTokensThatAreRefused(t *testing.T) {
	tests := []struct {
		name  string
		edit  func(map[string]any)
		forge string
		now   time.Time
	}{
		{name: "no back-channel logout event", edit: func(c map[string]any) { delete(c, "events") }},
		{name: "another event", edit: func(c map[string]any) {
			c["events"] = map[string]any{"http://schemas.openid.net/event/other": map[string]any{}}
		}},
		{name: "event that is not an object", edit: func(c map[string]any) {
			c["events"] = map[string]any{"http://schemas.openid.net/event/backchannel-logout": "yes"}
		}},
		{name: "nonce present", edit: func(c map[string]any) { c["nonce"] = "nonce-value" }},
		{name: "neither subject nor session", edit: func(c map[string]any) { delete(c, "sub"); delete(c, "sid") }},
		{name: "empty subject and session", edit: func(c map[string]any) { c["sub"] = ""; c["sid"] = "" }},
		{name: "missing expiry", edit: func(c map[string]any) { delete(c, "exp") }},
		{name: "past expiry", edit: func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() }},
		{name: "expired on the injected clock", now: time.Now().Add(time.Hour)},
		{name: "other issuer", edit: func(c map[string]any) { c["iss"] = "https://issuer.example" }},
		{name: "other audience", edit: func(c map[string]any) { c["aud"] = "other-client" }},
		{name: "unsigned", forge: "none"},
		{name: "HMAC with the client secret", forge: "HS256"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			idp := oidctest.New(t, oidctest.Options{})
			provider := discovered(t, idp, func(cfg *oidc.Config) {
				if !test.now.IsZero() {
					cfg.Now = func() time.Time { return test.now }
				}
			})
			idp.Forge(test.forge)

			_, err := provider.VerifyLogoutToken(context.Background(), idp.LogoutToken("subject-1", "provider-session-1", test.edit))

			require.ErrorIs(t, err, apperrors.ErrInvalidToken)
		})
	}
	t.Run("not a token", func(t *testing.T) {
		provider := discovered(t, oidctest.New(t, oidctest.Options{}))
		_, err := provider.VerifyLogoutToken(context.Background(), "not-a-token")
		require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	})
}

func TestLogoutTokenSignedWithARotatedKeyIsAccepted(t *testing.T) {
	idp := oidctest.New(t, oidctest.Options{})
	provider := discovered(t, idp)
	_, err := provider.VerifyLogoutToken(context.Background(), idp.LogoutToken("subject-1", "", nil))
	require.NoError(t, err)

	idp.RotateKeys()
	_, err = provider.VerifyLogoutToken(context.Background(), idp.LogoutToken("subject-1", "", nil))

	require.NoError(t, err)
}

func TestLogoutTokenVerificationIsUnavailableWithoutTheProvidersKeys(t *testing.T) {
	idp := oidctest.New(t, oidctest.Options{})
	provider := discovered(t, idp)
	idp.Override(oidctest.PathKeys, oidctest.Status(http.StatusServiceUnavailable))

	_, err := provider.VerifyLogoutToken(context.Background(), idp.LogoutToken("subject-1", "", nil))
	require.ErrorIs(t, err, apperrors.ErrUnavailable)

	undiscovered, err := oidc.NewProvider(fakeProviderConfig(idp))
	require.NoError(t, err)
	_, err = undiscovered.VerifyLogoutToken(context.Background(), idp.LogoutToken("subject-1", "", nil))
	require.ErrorIs(t, err, apperrors.ErrUnavailable)
}

func TestRefusedLogoutTokensAreLoggedWithAReasonAndNoToken(t *testing.T) {
	idp := oidctest.New(t, oidctest.Options{})
	logs := &logLines{}
	provider := discovered(t, idp, func(cfg *oidc.Config) { cfg.Logger = logs.logger() })
	token := idp.LogoutToken("subject-1", "provider-session-1", func(c map[string]any) { c["nonce"] = "nonce-value" })

	_, err := provider.VerifyLogoutToken(context.Background(), token)

	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	entries := logs.entries(t)
	require.Len(t, entries, 1)
	require.Equal(t, "oidc logout token rejected", entries[0]["msg"])
	require.Equal(t, "nonce", entries[0]["reason"])
	require.NotContains(t, logs.buf.String(), token)
}
