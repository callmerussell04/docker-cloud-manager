package jwt

import (
	"errors"
	"strings"

	"github.com/callmerussell04/docker-cloud-manager/pkg/jwtutils"
	"github.com/golang-jwt/jwt/v5"
)

type Parser struct {
	secretKey []byte
}

func NewParser(secretKey string) *Parser {
	return &Parser{
		secretKey: []byte(secretKey),
	}
}

func (p *Parser) ParseToken(authHeader string) (jwtutils.UserClaims, error) {
	if authHeader == "" {
		return jwtutils.UserClaims{}, errors.New("empty auth header")
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		return jwtutils.UserClaims{}, errors.New("invalid auth header format")
	}

	tokenStr := parts[1]

	token, err := jwt.ParseWithClaims(tokenStr, &jwtutils.UserClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return p.secretKey, nil
	})

	if err != nil || !token.Valid {
		return jwtutils.UserClaims{}, errors.New("invalid token")
	}

	claims, ok := token.Claims.(*jwtutils.UserClaims)
	if !ok {
		return jwtutils.UserClaims{}, errors.New("invalid claims format")
	}

	return *claims, nil
}
