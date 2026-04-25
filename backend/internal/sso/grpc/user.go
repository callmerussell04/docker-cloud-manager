package grpc

import (
	"context"

	sso "github.com/callmerussell04/docker-cloud-manager/api/sso"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/dto"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (h *Handler) GetUser(ctx context.Context, req *sso.GetUserRequest) (*sso.UserData, error) {
	userID, err := uuid.Parse(req.GetUserId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid user_id format")
	}
	userDTO := dto.GetUserRequest{UserID: userID}

	user, err := h.auth.GetUser(ctx, userDTO.UserID)
	if err != nil {
		return nil, apperrors.ToGRPC(err)
	}

	return userToProto(user), nil
}

func (h *Handler) BatchGetUsers(ctx context.Context, req *sso.BatchGetUsersRequest) (*sso.BatchGetUsersResponse, error) {
	ids := make([]uuid.UUID, 0, len(req.GetUserIds()))
	seen := make(map[uuid.UUID]struct{}, len(req.GetUserIds()))
	for _, rawID := range req.GetUserIds() {
		id, err := uuid.Parse(rawID)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid user_id format")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	usersDTO := dto.BatchGetUsersRequest{UserIDs: ids}

	users, err := h.auth.GetUsers(ctx, usersDTO.UserIDs)
	if err != nil {
		return nil, apperrors.ToGRPC(err)
	}

	resp := &sso.BatchGetUsersResponse{
		Users: make([]*sso.UserData, 0, len(users)),
	}
	for _, user := range users {
		resp.Users = append(resp.Users, userToProto(user))
	}
	return resp, nil
}
