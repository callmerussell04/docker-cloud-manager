package jwtutils

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type UserClaims struct {
	UserID    uuid.UUID `json:"sub"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	TokenType string    `json:"token_type"`
	jwt.RegisteredClaims
}
