package grpcclient

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sso "github.com/callmerussell04/docker-cloud-manager/api/sso"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/domain"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type SSOClient struct {
	api sso.AuthClient
}

func NewSSOClient(cc *grpc.ClientConn) *SSOClient {
	return &SSOClient{
		api: sso.NewAuthClient(cc),
	}
}

func (c *SSOClient) Register(ctx context.Context, username, email, password string) (string, error) {
	req := &sso.RegisterRequest{
		Username: username,
		Email:    email,
		Password: password,
	}

	resp, err := c.api.Register(ctx, req)
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			switch st.Code() {
			case codes.AlreadyExists:
				return "", apperrors.ErrAlreadyExists
			case codes.InvalidArgument:
				return "", apperrors.ErrBadRequest
			}
		}
		return "", apperrors.ErrInternal
	}

	return resp.GetUserId(), nil
}

func (c *SSOClient) Login(ctx context.Context, username, password string) (domain.Tokens, error) {
	req := &sso.LoginRequest{
		Username: username,
		Password: password,
	}

	resp, err := c.api.Login(ctx, req)
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			switch st.Code() {
			case codes.Unauthenticated:
				return domain.Tokens{}, apperrors.ErrInvalidCredentials
			case codes.InvalidArgument:
				return domain.Tokens{}, apperrors.ErrBadRequest
			}
		}
		return domain.Tokens{}, apperrors.ErrInternal
	}

	return domain.Tokens{
		AccessToken:  resp.GetAccessToken(),
		RefreshToken: resp.GetRefreshToken(),
	}, nil
}

func (c *SSOClient) Refresh(ctx context.Context, refreshToken string) (domain.Tokens, error) {
	req := &sso.RefreshRequest{
		RefreshToken: refreshToken,
	}

	resp, err := c.api.Refresh(ctx, req)
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			switch st.Code() {
			case codes.Unauthenticated:
				return domain.Tokens{}, apperrors.ErrInvalidToken
			case codes.InvalidArgument:
				return domain.Tokens{}, apperrors.ErrBadRequest
			}
		}
		return domain.Tokens{}, apperrors.ErrInternal
	}

	return domain.Tokens{
		AccessToken:  resp.GetAccessToken(),
		RefreshToken: resp.GetRefreshToken(),
	}, nil
}
