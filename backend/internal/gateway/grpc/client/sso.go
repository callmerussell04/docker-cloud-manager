package grpcclient

import (
	"context"
	"strings"

	"google.golang.org/grpc"

	sso "github.com/callmerussell04/docker-cloud-manager/api/sso"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
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
		return "", grpcerrors.FromGRPC(err)
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
		return model.Tokens{}, grpcerrors.FromGRPC(err)
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
		return model.Tokens{}, grpcerrors.FromGRPC(err)
	}

	return model.Tokens{
		AccessToken:  resp.GetAccessToken(),
		RefreshToken: resp.GetRefreshToken(),
	}, nil
}

func (c *SSOClient) GetAuthConfig(ctx context.Context) (model.AuthConfig, error) {
	resp, err := c.authAPI.GetAuthConfig(ctx, &sso.GetAuthConfigRequest{})
	if err != nil {
		return model.AuthConfig{}, grpcerrors.FromGRPC(err)
	}
	providers := make([]model.OIDCProvider, 0, len(resp.GetOidcProviders()))
	for _, provider := range resp.GetOidcProviders() {
		providers = append(providers, model.OIDCProvider{Name: provider.GetName()})
	}
	return model.AuthConfig{
		LocalLoginEnabled:    resp.GetLocalLoginEnabled(),
		LocalRegisterEnabled: resp.GetLocalRegisterEnabled(),
		OIDCProviders:        providers,
	}, nil
}

func (c *SSOClient) StartOIDCLogin(ctx context.Context, provider, redirectAfter string) (model.OIDCLoginStartResult, error) {
	resp, err := c.authAPI.StartOIDCLogin(ctx, &sso.StartOIDCLoginRequest{
		Provider:      provider,
		RedirectAfter: redirectAfter,
	})
	if err != nil {
		return model.OIDCLoginStartResult{}, grpcerrors.FromGRPC(err)
	}
	return model.OIDCLoginStartResult{
		AuthURL:      resp.GetAuthUrl(),
		StateBinding: resp.GetStateBinding(),
	}, nil
}

func (c *SSOClient) CompleteOIDCCallback(ctx context.Context, provider, code, state, stateBinding string) (model.OIDCCallbackResult, error) {
	resp, err := c.authAPI.CompleteOIDCCallback(ctx, &sso.CompleteOIDCCallbackRequest{
		Provider:     provider,
		Code:         code,
		State:        state,
		StateBinding: stateBinding,
	})
	if err != nil {
		return model.OIDCCallbackResult{}, grpcerrors.FromGRPC(err)
	}
	return model.OIDCCallbackResult{
		Tokens: model.Tokens{
			AccessToken:  resp.GetAccessToken(),
			RefreshToken: resp.GetRefreshToken(),
		},
		RedirectAfter: resp.GetRedirectAfter(),
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
		return model.AuthUser{}, grpcerrors.FromGRPC(err)
	}

	return model.AuthUser{
		UserID:   resp.GetUserId(),
		Username: resp.GetUsername(),
		Role:     resp.GetRole(),
	}, nil
}

func (c *SSOClient) CheckPermission(ctx context.Context, authHeader, permission string) (model.AuthUser, error) {
	token, err := accessTokenFromHeader(authHeader)
	if err != nil {
		return model.AuthUser{}, apperrors.ErrUnauthorized
	}

	resp, err := c.userAPI.CheckPermission(ctx, &sso.CheckPermissionRequest{
		AccessToken: token,
		Permission:  permission,
	})
	if err != nil {
		return model.AuthUser{}, grpcerrors.FromGRPC(err)
	}
	if !resp.GetAllowed() {
		return model.AuthUser{}, apperrors.ErrForbidden
	}
	user := resp.GetUser()
	if user == nil {
		return model.AuthUser{}, apperrors.ErrUnauthorized
	}
	return model.AuthUser{
		UserID:   user.GetUserId(),
		Username: user.GetUsername(),
		Role:     user.GetRole(),
	}, nil
}

func accessTokenFromHeader(authHeader string) (string, error) {
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" || parts[1] == "" {
		return "", apperrors.ErrUnauthorized
	}
	return parts[1], nil
}

func (c *SSOClient) ListUsers(ctx context.Context, page, limit int) (model.PaginatedAdminUsers, error) {
	resp, err := c.userAPI.ListUsers(ctx, &sso.ListUsersRequest{
		Page:  int32(page),
		Limit: int32(limit),
	})
	if err != nil {
		return model.PaginatedAdminUsers{}, grpcerrors.FromGRPC(err)
	}

	users := make([]model.AdminUser, 0, len(resp.GetUsers()))
	for _, pbUser := range resp.GetUsers() {
		users = append(users, adminUserFromProto(pbUser))
	}
	return model.PaginatedAdminUsers{
		Users:      users,
		TotalCount: resp.GetTotalCount(),
	}, nil
}

func (c *SSOClient) GetUser(ctx context.Context, userID string) (model.AdminUser, error) {
	resp, err := c.userAPI.GetUser(ctx, &sso.GetUserRequest{UserId: userID})
	if err != nil {
		return model.AdminUser{}, grpcerrors.FromGRPC(err)
	}
	return adminUserFromProto(resp), nil
}

func (c *SSOClient) CreateUser(ctx context.Context, input model.CreateUserInput) (model.AdminUser, error) {
	resp, err := c.userAPI.CreateUser(ctx, &sso.CreateUserRequest{
		Username:    input.Username,
		Email:       input.Email,
		Password:    input.Password,
		Role:        input.Role,
		Status:      input.Status,
		QuotaCpu:    input.QuotaCPU,
		QuotaRamMb:  input.QuotaRAMMB,
		QuotaDiskMb: input.QuotaDiskMB,
	})
	if err != nil {
		return model.AdminUser{}, grpcerrors.FromGRPC(err)
	}
	return adminUserFromProto(resp), nil
}

func (c *SSOClient) UpdateUser(ctx context.Context, userID string, input model.UpdateUserInput) (model.AdminUser, error) {
	resp, err := c.userAPI.UpdateUser(ctx, &sso.UpdateUserRequest{
		UserId:      userID,
		Username:    input.Username,
		Email:       input.Email,
		Password:    input.Password,
		Role:        input.Role,
		Status:      input.Status,
		QuotaCpu:    input.QuotaCPU,
		QuotaRamMb:  input.QuotaRAMMB,
		QuotaDiskMb: input.QuotaDiskMB,
	})
	if err != nil {
		return model.AdminUser{}, grpcerrors.FromGRPC(err)
	}
	return adminUserFromProto(resp), nil
}

func (c *SSOClient) DeactivateUser(ctx context.Context, userID string) (model.AdminUser, error) {
	resp, err := c.userAPI.DeactivateUser(ctx, &sso.UserIDRequest{UserId: userID})
	if err != nil {
		return model.AdminUser{}, grpcerrors.FromGRPC(err)
	}
	return adminUserFromProto(resp), nil
}

func (c *SSOClient) ReactivateUser(ctx context.Context, userID string) (model.AdminUser, error) {
	resp, err := c.userAPI.ReactivateUser(ctx, &sso.UserIDRequest{UserId: userID})
	if err != nil {
		return model.AdminUser{}, grpcerrors.FromGRPC(err)
	}
	return adminUserFromProto(resp), nil
}

func adminUserFromProto(user *sso.UserData) model.AdminUser {
	if user == nil {
		return model.AdminUser{}
	}
	return model.AdminUser{
		UserID:      user.GetUserId(),
		Username:    user.GetUsername(),
		Email:       user.GetEmail(),
		Role:        user.GetRole(),
		Status:      user.GetStatus(),
		QuotaCPU:    user.GetQuotaCpu(),
		QuotaRAMMB:  user.GetQuotaRamMb(),
		QuotaDiskMB: user.GetQuotaDiskMb(),
	}
}
