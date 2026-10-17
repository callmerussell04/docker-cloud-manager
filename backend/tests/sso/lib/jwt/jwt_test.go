package jwt_test

import (
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/sso/lib/jwt"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/jwtutils"
	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAccessTokensBindUniqueSessions(t *testing.T) {
	provider := jwt.NewProvider("secret")
	user := model.User{ID: uuid.New(), Username: "alice", Role: model.RoleAdmin}
	sessionID := uuid.New()
	token, err := provider.GenerateAccessToken(user, sessionID, time.Now().Add(time.Minute))
	require.NoError(t, err)
	identity, err := provider.ValidateAccessToken(token)
	require.NoError(t, err)
	require.Equal(t, model.AccessIdentity{UserID: user.ID, SessionID: sessionID}, identity)
	other, err := provider.GenerateAccessToken(user, uuid.New(), time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.NotEqual(t, token, other)
}

func TestProviderRejectsIncompleteOrWrongAccessClaims(t *testing.T) {
	provider := jwt.NewProvider("secret")
	valid := jwtutils.UserClaims{UserID: uuid.New(), SessionID: uuid.New(), TokenType: "access", RegisteredClaims: jwtlib.RegisteredClaims{ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(time.Minute))}}
	for _, name := range []string{"missing-session", "missing-user", "missing-expiry", "expired", "wrong-type", "wrong-algorithm", "wrong-key"} {
		t.Run(name, func(t *testing.T) {
			claims := valid
			algorithm := jwtlib.SigningMethodHS256
			key := "secret"
			switch name {
			case "missing-session":
				claims.SessionID = uuid.Nil
			case "missing-user":
				claims.UserID = uuid.Nil
			case "missing-expiry":
				claims.ExpiresAt = nil
			case "expired":
				claims.ExpiresAt = jwtlib.NewNumericDate(time.Now().Add(-time.Minute))
			case "wrong-type":
				claims.TokenType = "refresh"
			case "wrong-algorithm":
				algorithm = jwtlib.SigningMethodHS512
			case "wrong-key":
				key = "other"
			}
			token, err := jwtlib.NewWithClaims(algorithm, claims).SignedString([]byte(key))
			require.NoError(t, err)
			_, err = provider.ValidateAccessToken(token)
			require.Error(t, err)
		})
	}
	_, err := provider.ValidateAccessToken("not-a-jwt")
	require.Error(t, err)
}
