package grpc_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/api/sso"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	ssogrpc "github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/grpc"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
	ssoservice "github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
	ssogrpcmocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/sso/grpc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestSSOAuthGRPCMapsAuthRPCs(t *testing.T) {
	auth := ssogrpcmocks.NewMockAuthService(t)
	userID := uuid.New()
	oidcUserID := uuid.New()
	auth.EXPECT().Register(mock.Anything, "alice", "alice@example.com", "secret123").Return(userID, nil)
	auth.EXPECT().Login(mock.Anything, "alice", "secret123").Return(model.SignIn{
		Tokens: model.Tokens{AccessToken: "access", RefreshToken: "refresh", RefreshExpiresAt: time.Unix(2000000000, 0)},
		User:   model.SignedInUser{ID: userID, Username: "alice", Role: model.RoleAdmin},
	}, nil)
	auth.EXPECT().Refresh(mock.Anything, "refresh").Return(model.Tokens{AccessToken: "access2", RefreshToken: "refresh2", RefreshExpiresAt: time.Unix(2000000000, 0)}, nil)
	auth.EXPECT().LocalAuthConfig().Return(true, false)
	auth.EXPECT().AuthProviders().Return([]model.AuthProvider{{Name: "keycloak", Available: false}})
	auth.EXPECT().StartOIDCLogin(mock.Anything, "keycloak", "/dashboard").Return(model.OIDCLoginStart{
		AuthURL: "https://keycloak/auth", State: "state", StateBinding: "binding", ExpiresAt: time.Unix(2000000000, 0),
	}, nil)
	auth.EXPECT().CompleteOIDCCallback(mock.Anything, "keycloak", "code", "state", "binding").Return(model.OIDCCallbackResult{
		Outcome:       model.OIDCCallbackSuccess,
		Tokens:        model.Tokens{AccessToken: "access3", RefreshToken: "refresh3", RefreshExpiresAt: time.Unix(2000000000, 0)},
		User:          model.SignedInUser{ID: oidcUserID, Username: "bob", Role: model.RoleUser},
		RedirectAfter: "/dashboard",
	}, nil)
	conn := newSSOGRPCConn(t, auth, false)
	client := sso.NewAuthClient(conn)

	_, err := client.Register(context.Background(), &sso.RegisterRequest{})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	registerResp, err := client.Register(context.Background(), &sso.RegisterRequest{Username: "alice", Email: "alice@example.com", Password: "secret123"})
	require.NoError(t, err)
	require.Equal(t, userID.String(), registerResp.UserId)

	loginResp, err := client.Login(context.Background(), &sso.LoginRequest{Username: "alice", Password: "secret123"})
	require.NoError(t, err)
	require.Equal(t, "access", loginResp.AccessToken)
	require.Equal(t, "refresh", loginResp.RefreshToken)
	require.Equal(t, userID.String(), loginResp.GetUser().GetId())
	require.Equal(t, "alice", loginResp.GetUser().GetUsername())
	require.Equal(t, model.RoleAdmin, loginResp.GetUser().GetRole())

	refreshResp, err := client.Refresh(context.Background(), &sso.RefreshRequest{RefreshToken: "refresh"})
	require.NoError(t, err)
	require.Equal(t, "access2", refreshResp.AccessToken)

	cfgResp, err := client.GetAuthConfig(context.Background(), &sso.GetAuthConfigRequest{})
	require.NoError(t, err)
	require.True(t, cfgResp.LocalLoginEnabled)
	require.False(t, cfgResp.LocalRegisterEnabled)
	require.Len(t, cfgResp.OidcProviders, 1)
	require.Equal(t, "keycloak", cfgResp.OidcProviders[0].Name)
	require.False(t, cfgResp.OidcProviders[0].Available)

	startResp, err := client.StartOIDCLogin(context.Background(), &sso.StartOIDCLoginRequest{Provider: "keycloak", RedirectAfter: "/dashboard"})
	require.NoError(t, err)
	require.Equal(t, "https://keycloak/auth", startResp.AuthUrl)
	require.Equal(t, "binding", startResp.StateBinding)
	require.Equal(t, "state", startResp.State)
	require.Equal(t, int64(2000000000), startResp.ExpiresAt)

	callbackResp, err := client.CompleteOIDCCallback(context.Background(), &sso.CompleteOIDCCallbackRequest{Provider: "keycloak", Code: "code", State: "state", StateBinding: "binding"})
	require.NoError(t, err)
	require.Equal(t, sso.OIDCCallbackOutcome_OIDC_CALLBACK_OUTCOME_SUCCESS, callbackResp.Outcome)
	require.Equal(t, "access3", callbackResp.AccessToken)
	require.Equal(t, "/dashboard", callbackResp.RedirectAfter)
	require.Equal(t, oidcUserID.String(), callbackResp.GetUser().GetId())
	require.Equal(t, "bob", callbackResp.GetUser().GetUsername())
	require.Equal(t, model.RoleUser, callbackResp.GetUser().GetRole())
}

// Expected callback rejections are typed outcomes in the response, while
// availability and timeout failures stay gRPC errors.
func TestSSOAuthGRPCAnswersCallbackRejectionsAsOutcomes(t *testing.T) {
	for outcome, want := range map[model.OIDCCallbackOutcome]sso.OIDCCallbackOutcome{
		model.OIDCCallbackInvalidAttempt:     sso.OIDCCallbackOutcome_OIDC_CALLBACK_OUTCOME_INVALID_ATTEMPT,
		model.OIDCCallbackIdentityRejected:   sso.OIDCCallbackOutcome_OIDC_CALLBACK_OUTCOME_IDENTITY_REJECTED,
		model.OIDCCallbackAccountDeactivated: sso.OIDCCallbackOutcome_OIDC_CALLBACK_OUTCOME_ACCOUNT_DEACTIVATED,
		model.OIDCCallbackAccountConflict:    sso.OIDCCallbackOutcome_OIDC_CALLBACK_OUTCOME_ACCOUNT_CONFLICT,
	} {
		t.Run(string(outcome), func(t *testing.T) {
			auth := ssogrpcmocks.NewMockAuthService(t)
			auth.EXPECT().CompleteOIDCCallback(mock.Anything, "keycloak", "code", "state", "binding").
				Return(model.OIDCCallbackResult{Outcome: outcome, RedirectAfter: "/dashboard"}, nil)
			resp, err := sso.NewAuthClient(newSSOGRPCConn(t, auth, false)).CompleteOIDCCallback(context.Background(),
				&sso.CompleteOIDCCallbackRequest{Provider: "keycloak", Code: "code", State: "state", StateBinding: "binding"})
			require.NoError(t, err)
			require.Equal(t, want, resp.Outcome)
			require.Equal(t, "/dashboard", resp.RedirectAfter)
			require.Empty(t, resp.AccessToken)
			require.Empty(t, resp.RefreshToken)
		})
	}
	for failure, code := range map[error]codes.Code{apperrors.ErrUnavailable: codes.Unavailable, apperrors.ErrTimeout: codes.DeadlineExceeded} {
		t.Run(failure.Error(), func(t *testing.T) {
			auth := ssogrpcmocks.NewMockAuthService(t)
			auth.EXPECT().CompleteOIDCCallback(mock.Anything, "keycloak", "code", "state", "binding").Return(model.OIDCCallbackResult{}, failure)
			_, err := sso.NewAuthClient(newSSOGRPCConn(t, auth, false)).CompleteOIDCCallback(context.Background(),
				&sso.CompleteOIDCCallbackRequest{Provider: "keycloak", Code: "code", State: "state", StateBinding: "binding"})
			require.Equal(t, code, status.Code(err))
		})
	}
}

func TestSSOUserGRPCMapsReadAndPermissionRPCs(t *testing.T) {
	auth := ssogrpcmocks.NewMockAuthService(t)
	userID := uuid.New()
	otherID := uuid.New()
	user := model.User{ID: userID, Username: "alice", Email: "alice@example.com", Role: model.RoleAdmin, Status: model.StatusActive}
	auth.EXPECT().VerifyAccessToken(mock.Anything, "access").Return(user, nil)
	auth.EXPECT().CheckPermission(mock.Anything, "access", "users.admin.list").Return(user, true, nil)
	auth.EXPECT().GetUser(mock.Anything, userID).Return(user, nil)
	auth.EXPECT().GetUsers(mock.Anything, []uuid.UUID{userID, otherID}).Return([]model.User{user, {ID: otherID, Username: "bob", Email: "bob@example.com", Role: model.RoleUser, Status: model.StatusActive}}, nil)
	conn := newSSOGRPCConn(t, auth, false)
	authClient := sso.NewAuthClient(conn)
	userClient := sso.NewUserAPIClient(conn)

	_, err := userClient.VerifyAccessToken(context.Background(), &sso.VerifyTokenRequest{})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	verifyResp, err := userClient.VerifyAccessToken(context.Background(), &sso.VerifyTokenRequest{AccessToken: "access"})
	require.NoError(t, err)
	requireUserData(t, verifyResp, user)

	permResp, err := userClient.CheckPermission(context.Background(), &sso.CheckPermissionRequest{AccessToken: "access", Permission: "users.admin.list"})
	require.NoError(t, err)
	require.True(t, permResp.Allowed)
	requireUserData(t, permResp.User, user)

	getResp, err := userClient.GetUser(context.Background(), &sso.GetUserRequest{UserId: userID.String()})
	require.NoError(t, err)
	requireUserData(t, getResp, user)

	_, err = userClient.GetUser(context.Background(), &sso.GetUserRequest{UserId: "bad"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	batchResp, err := userClient.BatchGetUsers(context.Background(), &sso.BatchGetUsersRequest{UserIds: []string{userID.String(), userID.String(), otherID.String()}})
	require.NoError(t, err)
	require.Len(t, batchResp.Users, 2)

	_, err = authClient.StartOIDCLogin(context.Background(), &sso.StartOIDCLoginRequest{})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSSOUserGRPCAdminRoutes(t *testing.T) {
	auth := ssogrpcmocks.NewMockAuthService(t)
	userID := uuid.New()
	user := model.User{ID: userID, Username: "alice", Email: "alice@example.com", Role: model.RoleUser, Status: model.StatusActive}
	auth.EXPECT().ListUsers(mock.Anything, 20, 20).Return([]model.User{user}, 1, nil)
	auth.EXPECT().CreateUser(mock.Anything, mock.MatchedBy(func(input ssoservice.CreateUserInput) bool {
		return input.Username == "bob" && input.Role == model.RoleUser
	})).Return(user, nil)
	auth.EXPECT().UpdateUser(mock.Anything, userID, mock.MatchedBy(func(input ssoservice.UpdateUserInput) bool {
		return input.Username == "alice2" && input.Password == "newsecret"
	})).Return(user, nil)
	auth.EXPECT().DeactivateUser(mock.Anything, userID).Return(user, nil)
	auth.EXPECT().ReactivateUser(mock.Anything, userID).Return(user, nil)
	conn := newSSOGRPCConn(t, auth, true)
	client := sso.NewUserAPIClient(conn)

	listResp, err := client.ListUsers(context.Background(), &sso.ListUsersRequest{Page: 2})
	require.NoError(t, err)
	require.Equal(t, int32(1), listResp.TotalCount)
	require.Len(t, listResp.Users, 1)

	createResp, err := client.CreateUser(context.Background(), &sso.CreateUserRequest{Username: "bob", Email: "bob@example.com", Password: "secret123", Role: model.RoleUser, Status: model.StatusActive})
	require.NoError(t, err)
	requireUserData(t, createResp, user)

	updateResp, err := client.UpdateUser(context.Background(), &sso.UpdateUserRequest{UserId: userID.String(), Username: "alice2", Email: "alice2@example.com", Password: "newsecret", Role: model.RoleUser, Status: model.StatusActive})
	require.NoError(t, err)
	requireUserData(t, updateResp, user)

	_, err = client.DeactivateUser(context.Background(), &sso.UserIDRequest{UserId: userID.String()})
	require.NoError(t, err)
	_, err = client.ReactivateUser(context.Background(), &sso.UserIDRequest{UserId: userID.String()})
	require.NoError(t, err)

	_, err = client.UpdateUser(context.Background(), &sso.UpdateUserRequest{UserId: "bad"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// Every user message says where the account comes from and whether the
// identity provider owns its role.
func TestSSOUserGRPCReportsTheIdentitySourceAndRoleOwner(t *testing.T) {
	auth := ssogrpcmocks.NewMockAuthService(t)
	oidc := model.User{ID: uuid.New(), Username: "alice", Email: "alice@example.com", Role: model.RoleUser, Status: model.StatusActive, AuthSource: model.AuthSourceOIDC}
	local := model.User{ID: uuid.New(), Username: "bob", Email: "bob@example.com", Role: model.RoleUser, Status: model.StatusActive, AuthSource: model.AuthSourceLocal}
	auth.EXPECT().ListUsers(mock.Anything, 20, 0).Return([]model.User{oidc, local}, 2, nil)
	auth.EXPECT().RoleManagedByProvider(oidc).Return(true)
	auth.EXPECT().RoleManagedByProvider(local).Return(false)
	client := sso.NewUserAPIClient(newSSOGRPCConn(t, auth, true))

	resp, err := client.ListUsers(context.Background(), &sso.ListUsersRequest{})

	require.NoError(t, err)
	require.Equal(t, "oidc", resp.Users[0].GetAuthSource())
	require.True(t, resp.Users[0].GetRoleManagedByProvider())
	require.Equal(t, "local", resp.Users[1].GetAuthSource())
	require.False(t, resp.Users[1].GetRoleManagedByProvider())
}

func TestSSOUserGRPCRejectsAdminRoutesWithoutAdminScope(t *testing.T) {
	conn := newSSOGRPCConn(t, ssogrpcmocks.NewMockAuthService(t), false)
	client := sso.NewUserAPIClient(conn)

	_, err := client.ListUsers(context.Background(), &sso.ListUsersRequest{})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func newSSOGRPCConn(t *testing.T, auth ssogrpc.AuthService, injectAdmin bool) *grpc.ClientConn {
	t.Helper()
	if m, ok := auth.(*ssogrpcmocks.MockAuthService); ok {
		// Tests that do not ask about the role owner answer local ownership.
		m.EXPECT().RoleManagedByProvider(mock.Anything).Return(false).Maybe()
	}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if injectAdmin {
			ctx = accessscope.WithAdminScope(ctx, uuid.New(), "admin", model.RoleAdmin)
		}
		return handler(ctx, req)
	}))
	ssogrpc.Register(server, auth)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func requireUserData(t *testing.T, got *sso.UserData, want model.User) {
	t.Helper()
	require.Equal(t, want.ID.String(), got.UserId)
	require.Equal(t, want.Username, got.Username)
	require.Equal(t, want.Email, got.Email)
	require.Equal(t, want.Role, got.Role)
	require.Equal(t, want.Status, got.Status)
}

func TestSSOAuthGRPCMapsSignOutAndBackchannelLogout(t *testing.T) {
	auth := ssogrpcmocks.NewMockAuthService(t)
	auth.EXPECT().Logout(mock.Anything, "refresh", "http://frontend.example/login").Return("https://idp.example/logout", nil)
	auth.EXPECT().Logout(mock.Anything, "local-refresh", "").Return("", nil)
	auth.EXPECT().BackchannelLogout(mock.Anything, "keycloak", "logout-token").Return(2, nil)
	auth.EXPECT().BackchannelLogout(mock.Anything, "keycloak", "").Return(0, apperrors.ErrNotFound)
	auth.EXPECT().BackchannelLogout(mock.Anything, "keycloak", "refused").Return(0, apperrors.New(apperrors.ErrBadRequest, "invalid logout token"))
	auth.EXPECT().BackchannelLogout(mock.Anything, "keycloak", "keys-down").Return(0, apperrors.ErrUnavailable)
	client := sso.NewAuthClient(newSSOGRPCConn(t, auth, false))
	ctx := context.Background()

	signedOut, err := client.Logout(ctx, &sso.LogoutRequest{RefreshToken: "refresh", PostLogoutRedirectUri: "http://frontend.example/login"})
	require.NoError(t, err)
	require.Equal(t, "https://idp.example/logout", signedOut.GetProviderLogoutUrl())
	signedOut, err = client.Logout(ctx, &sso.LogoutRequest{RefreshToken: "local-refresh"})
	require.NoError(t, err)
	require.Empty(t, signedOut.GetProviderLogoutUrl())

	revoked, err := client.BackchannelLogout(ctx, &sso.BackchannelLogoutRequest{Provider: "keycloak", LogoutToken: "logout-token"})
	require.NoError(t, err)
	require.EqualValues(t, 2, revoked.GetRevokedSessions())
	_, err = client.BackchannelLogout(ctx, &sso.BackchannelLogoutRequest{LogoutToken: "logout-token"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	for token, code := range map[string]codes.Code{"": codes.NotFound, "refused": codes.InvalidArgument, "keys-down": codes.Unavailable} {
		_, err = client.BackchannelLogout(ctx, &sso.BackchannelLogoutRequest{Provider: "keycloak", LogoutToken: token})
		require.Equal(t, code, status.Code(err), token)
	}
}
