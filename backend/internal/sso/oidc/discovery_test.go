package oidc_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/oidc"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/oidctest"
	"github.com/stretchr/testify/require"
)

func fakeProviderConfig(idp *oidctest.Provider) oidc.Config {
	return oidc.Config{
		IssuerURL:    idp.Issuer(),
		ClientID:     idp.ClientID(),
		ClientSecret: idp.ClientSecret(),
		RedirectURL:  "http://gateway.example/callback",
		Transport:    idp.Transport(),
	}
}

func TestProviderIsUnavailableUntilDiscovered(t *testing.T) {
	idp := oidctest.New(t, oidctest.Options{Down: true})

	provider, err := oidc.NewProvider(fakeProviderConfig(idp))

	require.NoError(t, err, "construction makes no provider call")
	require.False(t, provider.Available())
	_, err = provider.ExchangeCode(context.Background(), "code", "nonce", "verifier")
	require.True(t, errors.Is(err, apperrors.ErrUnavailable), "got %v", err)
	require.Error(t, provider.Discover(context.Background()))
	require.False(t, provider.Available())

	idp.SetDown(false)
	require.NoError(t, provider.Discover(context.Background()))
	require.True(t, provider.Available())
}

// Discovery retries 1s, 2s, 4s... after the first attempt, so a provider that
// comes up at 3.5s is found by the attempt at 7s.
func TestRunRetriesDiscoveryWithDoublingBackoff(t *testing.T) {
	// The fake's listener lives outside the bubble; inside, only its in-process client is used.
	idp := oidctest.New(t, oidctest.Options{Down: true})
	synctest.Test(t, func(t *testing.T) {
		provider, err := oidc.NewProvider(fakeProviderConfig(idp))
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			provider.Run(ctx)
		}()

		time.Sleep(3500 * time.Millisecond)
		synctest.Wait()
		require.False(t, provider.Available())
		idp.SetDown(false)
		time.Sleep(3499 * time.Millisecond)
		synctest.Wait()
		require.False(t, provider.Available(), "no attempt between 3s and 7s")
		time.Sleep(time.Millisecond)
		synctest.Wait()
		require.True(t, provider.Available())

		<-done
		cancel()
	})
}

func TestRunCapsTheBackoffAtOneMinute(t *testing.T) {
	// The fake's listener lives outside the bubble; inside, only its in-process client is used.
	idp := oidctest.New(t, oidctest.Options{Down: true})
	synctest.Test(t, func(t *testing.T) {
		provider, err := oidc.NewProvider(fakeProviderConfig(idp))
		require.NoError(t, err)
		done := make(chan struct{})
		go func() {
			defer close(done)
			provider.Run(t.Context())
		}()

		time.Sleep(time.Hour)
		synctest.Wait()
		require.False(t, provider.Available())
		idp.SetDown(false)
		time.Sleep(time.Minute)
		synctest.Wait()
		require.True(t, provider.Available())
		<-done
	})
}

func TestRunStopsWithItsContext(t *testing.T) {
	// The fake's listener lives outside the bubble; inside, only its in-process client is used.
	idp := oidctest.New(t, oidctest.Options{Down: true})
	synctest.Test(t, func(t *testing.T) {
		provider, err := oidc.NewProvider(fakeProviderConfig(idp))
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			provider.Run(ctx)
		}()

		time.Sleep(10 * time.Second)
		cancel()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("discovery kept running after its context ended")
		}
		require.False(t, provider.Available())
	})
}
