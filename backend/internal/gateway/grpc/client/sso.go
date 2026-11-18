package client

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc"

	"github.com/callmerussell04/docker-cloud-manager/backend/api/sso"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/grpcerrors"
)

type SSOClient struct {
	authAPI sso.AuthClient
	userAPI sso.UserAPIClient
}

// withoutAuthenticatedUser states system scope for a call made before any
// user is authenticated: the public auth routes and access token checks. SSO
// refuses a call without a scope.
func withoutAuthenticatedUser(ctx context.Context) context.Context {
	return accessscope.WithSystemScope(ctx)
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

	resp, err := c.authAPI.Register(withoutAuthenticatedUser(ctx), req)
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

	resp, err := c.authAPI.Login(withoutAuthenticatedUser(ctx), req)
	if err != nil {
		return model.Tokens{}, grpcerrors.FromGRPC(err)
	}

	return model.Tokens{
		AccessToken:      resp.GetAccessToken(),
		RefreshToken:     resp.GetRefreshToken(),
		RefreshExpiresAt: time.Unix(resp.GetRefreshExpiresAt(), 0),
	}, nil
}

func (c *SSOClient) Refresh(ctx context.Context, refreshToken string) (model.Tokens, error) {
	req := &sso.RefreshRequest{
		RefreshToken: refreshToken,
	}

	resp, err := c.authAPI.Refresh(withoutAuthenticatedUser(ctx), req)
	if err != nil {
		return model.Tokens{}, grpcerrors.FromGRPC(err)
	}

	return model.Tokens{
		AccessToken:      resp.GetAccessToken(),
		RefreshToken:     resp.GetRefreshToken(),
		RefreshExpiresAt: time.Unix(resp.GetRefreshExpiresAt(), 0),
	}, nil
}

func (c *SSOClient) GetAuthConfig(ctx context.Context) (model.AuthConfig, error) {
	resp, err := c.authAPI.GetAuthConfig(withoutAuthenticatedUser(ctx), &sso.GetAuthConfigRequest{})
	if err != nil {
		return model.AuthConfig{}, grpcerrors.FromGRPC(err)
	}
	providers := make([]model.OIDCProvider, 0, len(resp.GetOidcProviders()))
	for _, provider := range resp.GetOidcProviders() {
		providers = append(providers, model.OIDCProvider{Name: provider.GetName(), Available: provider.GetAvailable()})
	}
	return model.AuthConfig{
		LocalLoginEnabled:    resp.GetLocalLoginEnabled(),
		LocalRegisterEnabled: resp.GetLocalRegisterEnabled(),
		OIDCProviders:        providers,
	}, nil
}

func (c *SSOClient) StartOIDCLogin(ctx context.Context, provider, redirectAfter string) (model.OIDCLoginStartResult, error) {
	resp, err := c.authAPI.StartOIDCLogin(withoutAuthenticatedUser(ctx), &sso.StartOIDCLoginRequest{
		Provider:      provider,
		RedirectAfter: redirectAfter,
	})
	if err != nil {
		return model.OIDCLoginStartResult{}, grpcerrors.FromGRPC(err)
	}
	return model.OIDCLoginStartResult{
		AuthURL:      resp.GetAuthUrl(),
		State:        resp.GetState(),
		StateBinding: resp.GetStateBinding(),
		ExpiresAt:    time.Unix(resp.GetExpiresAt(), 0),
	}, nil
}

func (c *SSOClient) CompleteOIDCCallback(ctx context.Context, provider, code, state, stateBinding string) (model.OIDCCallbackResult, error) {
	resp, err := c.authAPI.CompleteOIDCCallback(withoutAuthenticatedUser(ctx), &sso.CompleteOIDCCallbackRequest{
		Provider:     provider,
		Code:         code,
		State:        state,
		StateBinding: stateBinding,
	})
	if err != nil {
		return model.OIDCCallbackResult{}, grpcerrors.FromGRPC(err)
	}
	result := model.OIDCCallbackResult{
		Outcome:       callbackOutcomeFromProto(resp.GetOutcome()),
		RedirectAfter: resp.GetRedirectAfter(),
	}
	if result.Outcome == model.OIDCCallbackSuccess {
		result.Tokens = model.Tokens{
			AccessToken:      resp.GetAccessToken(),
			RefreshToken:     resp.GetRefreshToken(),
			RefreshExpiresAt: time.Unix(resp.GetRefreshExpiresAt(), 0),
		}
	}
	return result, nil
}

// callbackOutcomeFromProto maps an unknown outcome to the empty one, which
// Gateway treats as a failed sign-in.
func callbackOutcomeFromProto(outcome sso.OIDCCallbackOutcome) model.OIDCCallbackOutcome {
	switch outcome {
	case sso.OIDCCallbackOutcome_OIDC_CALLBACK_OUTCOME_SUCCESS:
		return model.OIDCCallbackSuccess
	case sso.OIDCCallbackOutcome_OIDC_CALLBACK_OUTCOME_INVALID_ATTEMPT:
		return model.OIDCCallbackInvalidAttempt
	case sso.OIDCCallbackOutcome_OIDC_CALLBACK_OUTCOME_IDENTITY_REJECTED:
		return model.OIDCCallbackIdentityRejected
	case sso.OIDCCallbackOutcome_OIDC_CALLBACK_OUTCOME_ACCOUNT_DEACTIVATED:
		return model.OIDCCallbackAccountDeactivated
	case sso.OIDCCallbackOutcome_OIDC_CALLBACK_OUTCOME_ACCOUNT_CONFLICT:
		return model.OIDCCallbackAccountConflict
	default:
		return ""
	}
}

func (c *SSOClient) VerifyAccessToken(ctx context.Context, authHeader string) (model.AuthUser, error) {
	token, err := accessTokenFromHeader(authHeader)
	if err != nil {
		return model.AuthUser{}, apperrors.ErrUnauthorized
	}

	resp, err := c.userAPI.VerifyAccessToken(withoutAuthenticatedUser(ctx), &sso.VerifyTokenRequest{
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

	resp, err := c.userAPI.CheckPermission(withoutAuthenticatedUser(ctx), &sso.CheckPermissionRequest{
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
		Username: input.Username,
		Email:    input.Email,
		Password: input.Password,
		Role:     input.Role,
		Status:   input.Status,
	})
	if err != nil {
		return model.AdminUser{}, grpcerrors.FromGRPC(err)
	}
	return adminUserFromProto(resp), nil
}

func (c *SSOClient) UpdateUser(ctx context.Context, userID string, input model.UpdateUserInput) (model.AdminUser, error) {
	resp, err := c.userAPI.UpdateUser(ctx, &sso.UpdateUserRequest{
		UserId:   userID,
		Username: input.Username,
		Email:    input.Email,
		Password: input.Password,
		Role:     input.Role,
		Status:   input.Status,
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
		UserID:   user.GetUserId(),
		Username: user.GetUsername(),
		Email:    user.GetEmail(),
		Role:     user.GetRole(),
		Status:   user.GetStatus(),

		AuthSource:            user.GetAuthSource(),
		RoleManagedByProvider: user.GetRoleManagedByProvider(),
	}
}

func (c *SSOClient) Logout(ctx context.Context, refreshToken string) error {
	_, err := c.authAPI.Logout(withoutAuthenticatedUser(ctx), &sso.LogoutRequest{RefreshToken: refreshToken})
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}
