package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
)

type SSOProvider interface {
	Register(ctx context.Context, username, email, password string) (string, error)
	Login(ctx context.Context, username, password string) (model.Tokens, error)
	Refresh(ctx context.Context, refreshToken string) (model.Tokens, error)
	GetAuthConfig(ctx context.Context) (model.AuthConfig, error)
	StartOIDCLogin(ctx context.Context, provider, redirectAfter string) (model.OIDCLoginStartResult, error)
	CompleteOIDCCallback(ctx context.Context, provider, code, state, stateBinding string) (model.OIDCCallbackResult, error)
	VerifyAccessToken(ctx context.Context, authHeader string) (model.AuthUser, error)
	CheckPermission(ctx context.Context, authHeader, permission string) (model.AuthUser, error)
}

type AuthService struct {
	sso SSOProvider
}

func NewAuth(sso SSOProvider) *AuthService {
	return &AuthService{
		sso: sso,
	}
}

func (s *AuthService) Register(ctx context.Context, username, email, password string) (string, error) {
	return s.sso.Register(ctx, username, email, password)
}

func (s *AuthService) Login(ctx context.Context, username, password string) (model.Tokens, error) {
	return s.sso.Login(ctx, username, password)
}

func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (model.Tokens, error) {
	return s.sso.Refresh(ctx, refreshToken)
}

func (s *AuthService) GetAuthConfig(ctx context.Context) (model.AuthConfig, error) {
	return s.sso.GetAuthConfig(ctx)
}

func (s *AuthService) StartOIDCLogin(ctx context.Context, provider, redirectAfter string) (model.OIDCLoginStartResult, error) {
	return s.sso.StartOIDCLogin(ctx, provider, redirectAfter)
}

func (s *AuthService) CompleteOIDCCallback(ctx context.Context, provider, code, state, stateBinding string) (model.OIDCCallbackResult, error) {
	return s.sso.CompleteOIDCCallback(ctx, provider, code, state, stateBinding)
}

func (s *AuthService) VerifyAccessToken(ctx context.Context, authHeader string) (model.AuthUser, error) {
	return s.sso.VerifyAccessToken(ctx, authHeader)
}

func (s *AuthService) CheckPermission(ctx context.Context, authHeader, permission string) (model.AuthUser, error) {
	return s.sso.CheckPermission(ctx, authHeader, permission)
}
