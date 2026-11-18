package grpc

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/backend/api/sso"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/grpcerrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/dto"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
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
		return nil, grpcerrors.ToGRPC(err)
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

	tokens, err := h.auth.Login(ctx, loginDTO.Username, loginDTO.Password)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &sso.LoginResponse{
		AccessToken:      tokens.AccessToken,
		RefreshExpiresAt: tokens.RefreshExpiresAt.Unix(),
		RefreshToken:     tokens.RefreshToken,
	}, nil
}

func (h *Handler) Refresh(ctx context.Context, req *sso.RefreshRequest) (*sso.RefreshResponse, error) {
	refreshDTO := dto.RefreshRequest{RefreshToken: req.GetRefreshToken()}

	tokens, err := h.auth.Refresh(ctx, refreshDTO.RefreshToken)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &sso.RefreshResponse{
		AccessToken:      tokens.AccessToken,
		RefreshExpiresAt: tokens.RefreshExpiresAt.Unix(),
		RefreshToken:     tokens.RefreshToken,
	}, nil
}

func (h *Handler) GetAuthConfig(ctx context.Context, _ *sso.GetAuthConfigRequest) (*sso.GetAuthConfigResponse, error) {
	loginEnabled, registerEnabled := h.auth.LocalAuthConfig()
	providers := h.auth.AuthProviders()

	resp := &sso.GetAuthConfigResponse{
		LocalLoginEnabled:    loginEnabled,
		LocalRegisterEnabled: registerEnabled,
		OidcProviders:        make([]*sso.OIDCProviderData, 0, len(providers)),
	}
	for _, provider := range providers {
		resp.OidcProviders = append(resp.OidcProviders, &sso.OIDCProviderData{Name: provider.Name, Available: provider.Available})
	}
	return resp, nil
}

func (h *Handler) StartOIDCLogin(ctx context.Context, req *sso.StartOIDCLoginRequest) (*sso.StartOIDCLoginResponse, error) {
	provider := req.GetProvider()
	if provider == "" {
		return nil, status.Error(codes.InvalidArgument, "missing provider")
	}

	started, err := h.auth.StartOIDCLogin(ctx, provider, req.GetRedirectAfter())
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	return &sso.StartOIDCLoginResponse{
		AuthUrl:      started.AuthURL,
		StateBinding: started.StateBinding,
		State:        started.State,
		ExpiresAt:    started.ExpiresAt.Unix(),
	}, nil
}

func (h *Handler) CompleteOIDCCallback(ctx context.Context, req *sso.CompleteOIDCCallbackRequest) (*sso.CompleteOIDCCallbackResponse, error) {
	if req.GetProvider() == "" || req.GetCode() == "" || req.GetState() == "" || req.GetStateBinding() == "" {
		return nil, status.Error(codes.InvalidArgument, "missing provider, code, state or state binding")
	}

	result, err := h.auth.CompleteOIDCCallback(ctx, req.GetProvider(), req.GetCode(), req.GetState(), req.GetStateBinding())
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	resp := &sso.CompleteOIDCCallbackResponse{
		Outcome:       callbackOutcomeToProto(result.Outcome),
		RedirectAfter: result.RedirectAfter,
	}
	if result.Outcome == model.OIDCCallbackSuccess {
		resp.AccessToken = result.Tokens.AccessToken
		resp.RefreshToken = result.Tokens.RefreshToken
		resp.RefreshExpiresAt = result.Tokens.RefreshExpiresAt.Unix()
	}
	return resp, nil
}

func callbackOutcomeToProto(outcome model.OIDCCallbackOutcome) sso.OIDCCallbackOutcome {
	switch outcome {
	case model.OIDCCallbackSuccess:
		return sso.OIDCCallbackOutcome_OIDC_CALLBACK_OUTCOME_SUCCESS
	case model.OIDCCallbackInvalidAttempt:
		return sso.OIDCCallbackOutcome_OIDC_CALLBACK_OUTCOME_INVALID_ATTEMPT
	case model.OIDCCallbackIdentityRejected:
		return sso.OIDCCallbackOutcome_OIDC_CALLBACK_OUTCOME_IDENTITY_REJECTED
	case model.OIDCCallbackAccountDeactivated:
		return sso.OIDCCallbackOutcome_OIDC_CALLBACK_OUTCOME_ACCOUNT_DEACTIVATED
	case model.OIDCCallbackAccountConflict:
		return sso.OIDCCallbackOutcome_OIDC_CALLBACK_OUTCOME_ACCOUNT_CONFLICT
	default:
		return sso.OIDCCallbackOutcome_OIDC_CALLBACK_OUTCOME_UNSPECIFIED
	}
}

func (h *Handler) VerifyAccessToken(ctx context.Context, req *sso.VerifyTokenRequest) (*sso.UserData, error) {
	tokenDTO := dto.VerifyTokenRequest{AccessToken: req.GetAccessToken()}
	if tokenDTO.AccessToken == "" {
		return nil, status.Error(codes.InvalidArgument, "missing access token")
	}

	user, err := h.auth.VerifyAccessToken(ctx, tokenDTO.AccessToken)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
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
		return nil, grpcerrors.ToGRPC(err)
	}

	return &sso.CheckPermissionResponse{
		Allowed: allowed,
		User:    userToProto(user),
	}, nil
}

func (h *Handler) Logout(ctx context.Context, req *sso.LogoutRequest) (*sso.LogoutResponse, error) {
	if err := h.auth.Logout(ctx, req.GetRefreshToken()); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	return &sso.LogoutResponse{}, nil
}
