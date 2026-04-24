package grpc

import (
	"context"
	"errors"

	"github.com/callmerussell04/docker-cloud-manager/internal/sso/domain"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sso "github.com/callmerussell04/docker-cloud-manager/api/sso"
)

type AuthService interface {
	Register(ctx context.Context, username, email, password string) (uuid.UUID, error)
	Login(ctx context.Context, username, password string) (string, string, error)
	Refresh(ctx context.Context, refreshToken string) (string, string, error)
	VerifyAccessToken(ctx context.Context, accessToken string) (domain.User, error)
	CheckPermission(ctx context.Context, accessToken, permission string) (domain.User, bool, error)
	GetUser(ctx context.Context, userID uuid.UUID) (domain.User, error)
	GetUsers(ctx context.Context, ids []uuid.UUID) ([]domain.User, error)
}

type Handler struct {
	sso.UnimplementedAuthServer
	sso.UnimplementedUserAPIServer
	auth AuthService
}

func Register(gRPCServer *grpc.Server, auth AuthService) {
	sso.RegisterAuthServer(gRPCServer, &Handler{auth: auth})
	sso.RegisterUserAPIServer(gRPCServer, &Handler{auth: auth})
}

func (h *Handler) Register(ctx context.Context, req *sso.RegisterRequest) (*sso.RegisterResponse, error) {
	if req.GetUsername() == "" || req.GetPassword() == "" || req.GetEmail() == "" {
		return nil, status.Error(codes.InvalidArgument, "missing required fields")
	}

	uid, err := h.auth.Register(ctx, req.GetUsername(), req.GetEmail(), req.GetPassword())
	if err != nil {
		if errors.Is(err, apperrors.ErrAlreadyExists) {
			return nil, status.Error(codes.AlreadyExists, "user already exists")
		}
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &sso.RegisterResponse{
		UserId: uid.String(),
	}, nil
}

func (h *Handler) Login(ctx context.Context, req *sso.LoginRequest) (*sso.LoginResponse, error) {
	if req.GetUsername() == "" || req.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "missing required fields")
	}

	accessToken, refreshToken, err := h.auth.Login(ctx, req.GetUsername(), req.GetPassword())
	if err != nil {
		if errors.Is(err, apperrors.ErrInvalidCredentials) {
			return nil, status.Error(codes.Unauthenticated, "invalid credentials")
		}
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &sso.LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

func (h *Handler) Refresh(ctx context.Context, req *sso.RefreshRequest) (*sso.RefreshResponse, error) {
	if req.GetRefreshToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "missing refresh token")
	}

	accessToken, refreshToken, err := h.auth.Refresh(ctx, req.GetRefreshToken())
	if err != nil {
		if errors.Is(err, apperrors.ErrInvalidToken) {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &sso.RefreshResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

func (h *Handler) VerifyAccessToken(ctx context.Context, req *sso.VerifyTokenRequest) (*sso.UserData, error) {
	if req.GetAccessToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "missing access token")
	}

	user, err := h.auth.VerifyAccessToken(ctx, req.GetAccessToken())
	if err != nil {
		if errors.Is(err, apperrors.ErrInvalidToken) {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}
		return nil, status.Error(codes.Internal, "internal error")
	}

	return userToProto(user), nil
}

func (h *Handler) CheckPermission(ctx context.Context, req *sso.CheckPermissionRequest) (*sso.CheckPermissionResponse, error) {
	if req.GetAccessToken() == "" || req.GetPermission() == "" {
		return nil, status.Error(codes.InvalidArgument, "missing access token or permission")
	}

	user, allowed, err := h.auth.CheckPermission(ctx, req.GetAccessToken(), req.GetPermission())
	if err != nil {
		if errors.Is(err, apperrors.ErrInvalidToken) {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}
		if errors.Is(err, apperrors.ErrBadRequest) {
			return nil, status.Error(codes.InvalidArgument, "unknown permission")
		}
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &sso.CheckPermissionResponse{
		Allowed: allowed,
		User:    userToProto(user),
	}, nil
}

func (h *Handler) GetUser(ctx context.Context, req *sso.GetUserRequest) (*sso.UserData, error) {
	userID, err := uuid.Parse(req.GetUserId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid user_id format")
	}

	user, err := h.auth.GetUser(ctx, userID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		return nil, status.Error(codes.Internal, "internal error")
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

	users, err := h.auth.GetUsers(ctx, ids)
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}

	resp := &sso.BatchGetUsersResponse{
		Users: make([]*sso.UserData, 0, len(users)),
	}
	for _, user := range users {
		resp.Users = append(resp.Users, userToProto(user))
	}
	return resp, nil
}

func userToProto(user domain.User) *sso.UserData {
	return &sso.UserData{
		UserId:      user.ID.String(),
		Username:    user.Username,
		Email:       user.Email,
		Role:        user.Role,
		QuotaRamMb:  user.QuotaRAMMB,
		QuotaDiskMb: user.QuotaDiskMB,
		QuotaCpu:    user.QuotaCPU,
	}
}
