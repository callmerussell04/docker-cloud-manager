package jwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/jwtutils"
)

const (
	tokenTypeAccess  = "access"
	tokenTypeRefresh = "refresh"
)

type Provider struct {
	secretKey  []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewProvider(secretKey string, accessTTL, refreshTTL time.Duration) *Provider {
	return &Provider{
		secretKey:  []byte(secretKey),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

func (p *Provider) GenerateTokens(user model.User) (string, string, error) {
	accessClaims := jwtutils.UserClaims{
		UserID:    user.ID,
		Username:  user.Username,
		Role:      user.Role,
		TokenType: tokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(p.accessTTL)),
		},
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessTokenStr, err := accessToken.SignedString(p.secretKey)
	if err != nil {
		return "", "", err
	}

	refreshClaims := jwtutils.UserClaims{
		UserID:    user.ID,
		TokenType: tokenTypeRefresh,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(p.refreshTTL)),
		},
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenStr, err := refreshToken.SignedString(p.secretKey)
	if err != nil {
		return "", "", err
	}

	return accessTokenStr, refreshTokenStr, nil
}

func (p *Provider) ValidateRefreshToken(tokenStr string) (uuid.UUID, error) {
	return p.validateToken(tokenStr, tokenTypeRefresh)
}

func (p *Provider) ValidateAccessToken(tokenStr string) (uuid.UUID, error) {
	return p.validateToken(tokenStr, tokenTypeAccess)
}

func (p *Provider) validateToken(tokenStr, expectedType string) (uuid.UUID, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &jwtutils.UserClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return p.secretKey, nil
	})

	if err != nil || !token.Valid {
		return uuid.Nil, errors.New("invalid token")
	}

	claims, ok := token.Claims.(*jwtutils.UserClaims)
	if !ok {
		return uuid.Nil, errors.New("invalid claims")
	}
	if claims.TokenType != expectedType {
		return uuid.Nil, fmt.Errorf("unexpected token type: %s", claims.TokenType)
	}

	return claims.UserID, nil
}
