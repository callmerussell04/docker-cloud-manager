package grpc

import (
	"context"

	sso "github.com/callmerussell04/docker-cloud-manager/api/sso"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/model"
	ssoservice "github.com/callmerussell04/docker-cloud-manager/internal/sso/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
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
		return nil, grpcerrors.ToGRPC(err)
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
		return nil, grpcerrors.ToGRPC(err)
	}

	resp := &sso.BatchGetUsersResponse{
		Users: make([]*sso.UserData, 0, len(users)),
	}
	for _, user := range users {
		resp.Users = append(resp.Users, userToProto(user))
	}
	return resp, nil
}

func (h *Handler) ListUsers(ctx context.Context, req *sso.ListUsersRequest) (*sso.ListUsersResponse, error) {
	if err := requireAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	page, limit := pagination(req.GetPage(), req.GetLimit())
	usersDTO := dto.ListUsersRequest{Page: page, Limit: limit}
	offset := (usersDTO.Page - 1) * usersDTO.Limit

	users, total, err := h.auth.ListUsers(ctx, usersDTO.Limit, offset)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	resp := &sso.ListUsersResponse{
		Users:      make([]*sso.UserData, 0, len(users)),
		TotalCount: int32(total),
	}
	for _, user := range users {
		resp.Users = append(resp.Users, userToProto(user))
	}
	return resp, nil
}

func (h *Handler) CreateUser(ctx context.Context, req *sso.CreateUserRequest) (*sso.UserData, error) {
	if err := requireAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	createDTO := dto.CreateUserRequest{
		Username:    req.GetUsername(),
		Email:       req.GetEmail(),
		Password:    req.GetPassword(),
		Role:        req.GetRole(),
		Status:      req.GetStatus(),
		QuotaCPU:    req.GetQuotaCpu(),
		QuotaRAMMB:  req.GetQuotaRamMb(),
		QuotaDiskMB: req.GetQuotaDiskMb(),
	}
	if createDTO.Username == "" || createDTO.Email == "" || createDTO.Password == "" {
		return nil, status.Error(codes.InvalidArgument, "missing required fields")
	}

	user, err := h.auth.CreateUser(ctx, ssoservice.CreateUserInput(createDTO))
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	return userToProto(user), nil
}

func (h *Handler) UpdateUser(ctx context.Context, req *sso.UpdateUserRequest) (*sso.UserData, error) {
	if err := requireAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	userID, err := uuid.Parse(req.GetUserId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid user_id format")
	}
	updateDTO := dto.UpdateUserRequest{
		UserID:      userID,
		Username:    req.GetUsername(),
		Email:       req.GetEmail(),
		Password:    req.GetPassword(),
		Role:        req.GetRole(),
		Status:      req.GetStatus(),
		QuotaCPU:    req.GetQuotaCpu(),
		QuotaRAMMB:  req.GetQuotaRamMb(),
		QuotaDiskMB: req.GetQuotaDiskMb(),
	}
	if updateDTO.Username == "" || updateDTO.Email == "" {
		return nil, status.Error(codes.InvalidArgument, "missing required fields")
	}

	user, err := h.auth.UpdateUser(ctx, updateDTO.UserID, ssoservice.UpdateUserInput{
		Username:    updateDTO.Username,
		Email:       updateDTO.Email,
		Password:    updateDTO.Password,
		Role:        updateDTO.Role,
		Status:      updateDTO.Status,
		QuotaCPU:    updateDTO.QuotaCPU,
		QuotaRAMMB:  updateDTO.QuotaRAMMB,
		QuotaDiskMB: updateDTO.QuotaDiskMB,
	})
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	return userToProto(user), nil
}

func (h *Handler) DeactivateUser(ctx context.Context, req *sso.UserIDRequest) (*sso.UserData, error) {
	return h.setUserActive(ctx, req, false)
}

func (h *Handler) ReactivateUser(ctx context.Context, req *sso.UserIDRequest) (*sso.UserData, error) {
	return h.setUserActive(ctx, req, true)
}

func (h *Handler) setUserActive(ctx context.Context, req *sso.UserIDRequest, active bool) (*sso.UserData, error) {
	if err := requireAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	userID, err := uuid.Parse(req.GetUserId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid user_id format")
	}

	var user model.User
	if active {
		user, err = h.auth.ReactivateUser(ctx, userID)
	} else {
		user, err = h.auth.DeactivateUser(ctx, userID)
	}
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	return userToProto(user), nil
}

func pagination(pageRaw, limitRaw int32) (int, int) {
	page := int(pageRaw)
	if page < 1 {
		page = 1
	}
	limit := int(limitRaw)
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return page, limit
}

func requireAdminScope(ctx context.Context) error {
	scope, ok := accessscope.FromContext(ctx)
	if !ok || scope.Kind != accessscope.KindAdmin {
		return apperrors.ErrForbidden
	}
	return nil
}
