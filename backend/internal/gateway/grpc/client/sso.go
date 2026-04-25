package grpcclient

import (
	"context"
	"strings"

	"google.golang.org/grpc"

	sso "github.com/callmerussell04/docker-cloud-manager/api/sso"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type SSOClient struct {
	authAPI sso.AuthClient
	userAPI sso.UserAPIClient
}

func NewSSOClient(cc *grpc.ClientConn) *SSOClient {
	return &SSOClient{
		authAPI: sso.NewAuthClient(cc),
		userAPI: sso.NewUserAPIClient(cc),
	}
}

func (c *SSOClient) Register(ctx context.Context, username, email, password string) (string, error) {
	req := &sso.RegisterRequest{
		Username: username,
		Email:    email,
		Password: password,
	}

	resp, err := c.authAPI.Register(ctx, req)
	if err != nil {
		return "", apperrors.FromGRPC(err)
	}

	return resp.GetUserId(), nil
}

func (c *SSOClient) Login(ctx context.Context, username, password string) (model.Tokens, error) {
	req := &sso.LoginRequest{
		Username: username,
		Password: password,
	}

	resp, err := c.authAPI.Login(ctx, req)
	if err != nil {
		return model.Tokens{}, apperrors.FromGRPC(err)
	}

	return model.Tokens{
		AccessToken:  resp.GetAccessToken(),
		RefreshToken: resp.GetRefreshToken(),
	}, nil
}

func (c *SSOClient) Refresh(ctx context.Context, refreshToken string) (model.Tokens, error) {
	req := &sso.RefreshRequest{
		RefreshToken: refreshToken,
	}

	resp, err := c.authAPI.Refresh(ctx, req)
	if err != nil {
		return model.Tokens{}, apperrors.FromGRPC(err)
	}

	return model.Tokens{
		AccessToken:  resp.GetAccessToken(),
		RefreshToken: resp.GetRefreshToken(),
	}, nil
}

func (c *SSOClient) VerifyAccessToken(ctx context.Context, authHeader string) (model.AuthUser, error) {
	token, err := accessTokenFromHeader(authHeader)
	if err != nil {
		return model.AuthUser{}, apperrors.ErrUnauthorized
	}

	resp, err := c.userAPI.VerifyAccessToken(ctx, &sso.VerifyTokenRequest{
		AccessToken: token,
	})
	if err != nil {
		return model.AuthUser{}, apperrors.FromGRPC(err)
	}

	return model.AuthUser{
		UserID:   resp.GetUserId(),
		Username: resp.GetUsername(),
		Role:     resp.GetRole(),
	}, nil
}

func (c *SSOClient) CheckPermission(ctx context.Context, authHeader, permission string) error {
	token, err := accessTokenFromHeader(authHeader)
	if err != nil {
		return apperrors.ErrUnauthorized
	}

	resp, err := c.userAPI.CheckPermission(ctx, &sso.CheckPermissionRequest{
		AccessToken: token,
		Permission:  permission,
	})
	if err != nil {
		return apperrors.FromGRPC(err)
	}
	if !resp.GetAllowed() {
		return apperrors.ErrForbidden
	}
	return nil
}

func accessTokenFromHeader(authHeader string) (string, error) {
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" || parts[1] == "" {
		return "", apperrors.ErrUnauthorized
	}
	return parts[1], nil
}
