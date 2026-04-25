package grpc

import (
	"context"
	"errors"

	sso "github.com/callmerussell04/docker-cloud-manager/api/sso"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/dto"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (h *Handler) Register(ctx context.Context, req *sso.RegisterRequest) (*sso.RegisterResponse, error) {
	registerDTO := dto.RegisterRequest{
		Username: req.GetUsername(),
		Email:    req.GetEmail(),
		Password: req.GetPassword(),
	}
	if registerDTO.Username == "" || registerDTO.Password == "" || registerDTO.Email == "" {
		return nil, status.Error(codes.InvalidArgument, "missing required fields")
	}

	uid, err := h.auth.Register(ctx, registerDTO.Username, registerDTO.Email, registerDTO.Password)
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
	loginDTO := dto.LoginRequest{
		Username: req.GetUsername(),
		Password: req.GetPassword(),
	}
	if loginDTO.Username == "" || loginDTO.Password == "" {
		return nil, status.Error(codes.InvalidArgument, "missing required fields")
	}

	accessToken, refreshToken, err := h.auth.Login(ctx, loginDTO.Username, loginDTO.Password)
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
	refreshDTO := dto.RefreshRequest{RefreshToken: req.GetRefreshToken()}
	if refreshDTO.RefreshToken == "" {
		return nil, status.Error(codes.InvalidArgument, "missing refresh token")
	}

	accessToken, refreshToken, err := h.auth.Refresh(ctx, refreshDTO.RefreshToken)
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
	tokenDTO := dto.VerifyTokenRequest{AccessToken: req.GetAccessToken()}
	if tokenDTO.AccessToken == "" {
		return nil, status.Error(codes.InvalidArgument, "missing access token")
	}

	user, err := h.auth.VerifyAccessToken(ctx, tokenDTO.AccessToken)
	if err != nil {
		if errors.Is(err, apperrors.ErrInvalidToken) {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}
		return nil, status.Error(codes.Internal, "internal error")
	}

	return userToProto(user), nil
}

func (h *Handler) CheckPermission(ctx context.Context, req *sso.CheckPermissionRequest) (*sso.CheckPermissionResponse, error) {
	checkDTO := dto.CheckPermissionRequest{
		AccessToken: req.GetAccessToken(),
		Permission:  req.GetPermission(),
	}
	if checkDTO.AccessToken == "" || checkDTO.Permission == "" {
		return nil, status.Error(codes.InvalidArgument, "missing access token or permission")
	}

	user, allowed, err := h.auth.CheckPermission(ctx, checkDTO.AccessToken, checkDTO.Permission)
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
