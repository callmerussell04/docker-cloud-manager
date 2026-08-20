package model

import "github.com/google/uuid"

type ListOptions struct {
	OwnerID   *uuid.UUID
	ProjectID *uuid.UUID
	Limit     int
	Offset    int
}
