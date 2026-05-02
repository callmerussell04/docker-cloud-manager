package dto

import "github.com/google/uuid"

type GetUserRequest struct {
	UserID uuid.UUID
}

type BatchGetUsersRequest struct {
	UserIDs []uuid.UUID
}

type ListUsersRequest struct {
	Page  int
	Limit int
}

type CreateUserRequest struct {
	Username    string
	Email       string
	Password    string
	Role        string
	Status      string
	QuotaCPU    float64
	QuotaRAMMB  int64
	QuotaDiskMB int64
}

type UpdateUserRequest struct {
	UserID      uuid.UUID
	Username    string
	Email       string
	Password    string
	Role        string
	Status      string
	QuotaCPU    float64
	QuotaRAMMB  int64
	QuotaDiskMB int64
}
