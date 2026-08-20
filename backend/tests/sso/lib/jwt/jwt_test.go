package jwt_test

import (
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/sso/lib/jwt"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestProviderGeneratesAndValidatesAccessAndRefreshTokens(t *testing.T) {
	provider := jwt.NewProvider("secret", time.Minute, time.Hour)
	userID := uuid.New()

	accessToken, refreshToken, err := provider.GenerateTokens(model.User{
		ID:       userID,
		Username: "alice",
		Role:     model.RoleAdmin,
	})
	require.NoError(t, err)

	gotAccessID, err := provider.ValidateAccessToken(accessToken)
	require.NoError(t, err)
	require.Equal(t, userID, gotAccessID)

	gotRefreshID, err := provider.ValidateRefreshToken(refreshToken)
	require.NoError(t, err)
	require.Equal(t, userID, gotRefreshID)
}

func TestProviderRejectsInvalidTokens(t *testing.T) {
	provider := jwt.NewProvider("secret", time.Minute, time.Hour)
	otherProvider := jwt.NewProvider("other-secret", time.Minute, time.Hour)
	expiredProvider := jwt.NewProvider("secret", -time.Minute, -time.Minute)
	userID := uuid.New()

	accessToken, refreshToken, err := provider.GenerateTokens(model.User{ID: userID})
	require.NoError(t, err)
	expiredAccess, _, err := expiredProvider.GenerateTokens(model.User{ID: userID})
	require.NoError(t, err)

	_, err = provider.ValidateRefreshToken(accessToken)
	require.Error(t, err)

	_, err = provider.ValidateAccessToken(refreshToken)
	require.Error(t, err)

	_, err = otherProvider.ValidateAccessToken(accessToken)
	require.Error(t, err)

	_, err = provider.ValidateAccessToken("not-a-jwt")
	require.Error(t, err)

	_, err = provider.ValidateAccessToken(expiredAccess)
	require.Error(t, err)
}
