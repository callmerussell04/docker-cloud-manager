package jwt

import (
	"errors"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/platform/jwtutils"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/model"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Provider struct{ secretKey []byte }

func NewProvider(secretKey string) *Provider {
	return &Provider{secretKey: []byte(secretKey)}
}

func (p *Provider) GenerateAccessToken(user model.User, sessionID uuid.UUID, expiresAt time.Time) (string, error) {
	claims := jwtutils.UserClaims{
		UserID: user.ID, SessionID: sessionID, Username: user.Username, Role: user.Role, TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{ID: uuid.NewString(), ExpiresAt: jwt.NewNumericDate(expiresAt)},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(p.secretKey)
}

func (p *Provider) ValidateAccessToken(value string) (model.AccessIdentity, error) {
	claims := &jwtutils.UserClaims{}
	token, err := jwt.ParseWithClaims(value, claims, func(_ *jwt.Token) (any, error) {
		return p.secretKey, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid || claims.TokenType != "access" || claims.UserID == uuid.Nil || claims.SessionID == uuid.Nil {
		return model.AccessIdentity{}, errors.New("invalid access token")
	}
	return model.AccessIdentity{UserID: claims.UserID, SessionID: claims.SessionID}, nil
}
