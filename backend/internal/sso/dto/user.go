package dto

import "github.com/google/uuid"

type GetUserRequest struct {
	UserID uuid.UUID
}

type BatchGetUsersRequest struct {
	UserIDs []uuid.UUID
}
