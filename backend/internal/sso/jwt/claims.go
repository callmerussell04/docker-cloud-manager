package jwt

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type UserClaims struct {
	SessionID uuid.UUID `json:"sid"`
	UserID    uuid.UUID `json:"sub"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	TokenType string    `json:"token_type"`
	jwt.RegisteredClaims
}
