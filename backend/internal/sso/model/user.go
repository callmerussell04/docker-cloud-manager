package model

import "github.com/google/uuid"

const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

type User struct {
	ID           uuid.UUID
	Username     string
	Email        string
	PasswordHash string
	Role         string
	QuotaCPU     float64
	QuotaRAMMB   int64
	QuotaDiskMB  int64
}
