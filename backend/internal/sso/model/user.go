package model

import "github.com/google/uuid"

const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

const (
	StatusActive      = "active"
	StatusDeactivated = "deactivated"
)

type User struct {
	ID           uuid.UUID
	Username     string
	Email        string
	PasswordHash string
	Role         string
	Status       string
	QuotaCPU     float64
	QuotaRAMMB   int64
	QuotaDiskMB  int64
}

type ListUsersOptions struct {
	Limit  int
	Offset int
}
