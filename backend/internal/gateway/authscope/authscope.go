package authscope

import (
	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
)

func FromAuthUser(user model.AuthUser, kind accessscope.Kind) (accessscope.Scope, error) {
	userID, err := uuid.Parse(user.UserID)
	if err != nil {
		return accessscope.Scope{}, err
	}
	return accessscope.Scope{
		Kind:     kind,
		UserID:   userID,
		Username: user.Username,
		Role:     user.Role,
	}, nil
}
