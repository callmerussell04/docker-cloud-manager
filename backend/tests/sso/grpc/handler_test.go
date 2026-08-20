package grpc_test

import (
	"context"
	"net"
	"testing"

	ssoapi "github.com/callmerussell04/docker-cloud-manager/api/sso"
	ssogrpc "github.com/callmerussell04/docker-cloud-manager/internal/sso/grpc"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/model"
	ssoservice "github.com/callmerussell04/docker-cloud-manager/internal/sso/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	ssogrpcmocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/sso/grpc"
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
	auth.EXPECT().Register(mock.Anything, "alice", "alice@example.com", "secret123").Return(userID, nil)
	auth.EXPECT().Login(mock.Anything, "alice", "secret123").Return("access", "refresh", nil)
	auth.EXPECT().Refresh(mock.Anything, "refresh").Return("access2", "refresh2", nil)
	auth.EXPECT().LocalAuthConfig().Return(true, false)
	auth.EXPECT().AuthProviders().Return([]string{"keycloak"})
	auth.EXPECT().StartOIDCLogin(mock.Anything, "keycloak", "/dashboard").Return("https://keycloak/auth", "binding", nil)
	auth.EXPECT().CompleteOIDCCallback(mock.Anything, "keycloak", "code", "state", "binding").Return("access3", "refresh3", "/dashboard", nil)
	conn := newSSOGRPCConn(t, auth, false)
	client := ssoapi.NewAuthClient(conn)

	_, err := client.Register(context.Background(), &ssoapi.RegisterRequest{})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	registerResp, err := client.Register(context.Background(), &ssoapi.RegisterRequest{Username: "alice", Email: "alice@example.com", Password: "secret123"})
	require.NoError(t, err)
	require.Equal(t, userID.String(), registerResp.UserId)

	loginResp, err := client.Login(context.Background(), &ssoapi.LoginRequest{Username: "alice", Password: "secret123"})
	require.NoError(t, err)
	require.Equal(t, "access", loginResp.AccessToken)
	require.Equal(t, "refresh", loginResp.RefreshToken)

	refreshResp, err := client.Refresh(context.Background(), &ssoapi.RefreshRequest{RefreshToken: "refresh"})
	require.NoError(t, err)
	require.Equal(t, "access2", refreshResp.AccessToken)

	cfgResp, err := client.GetAuthConfig(context.Background(), &ssoapi.GetAuthConfigRequest{})
	require.NoError(t, err)
	require.True(t, cfgResp.LocalLoginEnabled)
	require.False(t, cfgResp.LocalRegisterEnabled)
	require.Len(t, cfgResp.OidcProviders, 1)
	require.Equal(t, "keycloak", cfgResp.OidcProviders[0].Name)

	startResp, err := client.StartOIDCLogin(context.Background(), &ssoapi.StartOIDCLoginRequest{Provider: "keycloak", RedirectAfter: "/dashboard"})
	require.NoError(t, err)
	require.Equal(t, "https://keycloak/auth", startResp.AuthUrl)
	require.Equal(t, "binding", startResp.StateBinding)

	callbackResp, err := client.CompleteOIDCCallback(context.Background(), &ssoapi.CompleteOIDCCallbackRequest{Provider: "keycloak", Code: "code", State: "state", StateBinding: "binding"})
	require.NoError(t, err)
	require.Equal(t, "access3", callbackResp.AccessToken)
	require.Equal(t, "/dashboard", callbackResp.RedirectAfter)
}

func TestSSOUserGRPCMapsReadAndPermissionRPCs(t *testing.T) {
	auth := ssogrpcmocks.NewMockAuthService(t)
	userID := uuid.New()
	otherID := uuid.New()
	user := model.User{ID: userID, Username: "alice", Email: "alice@example.com", Role: model.RoleAdmin, Status: model.StatusActive, QuotaCPU: 2, QuotaRAMMB: 4096, QuotaDiskMB: 8192}
	auth.EXPECT().VerifyAccessToken(mock.Anything, "access").Return(user, nil)
	auth.EXPECT().CheckPermission(mock.Anything, "access", "users.admin.list").Return(user, true, nil)
	auth.EXPECT().GetUser(mock.Anything, userID).Return(user, nil)
	auth.EXPECT().GetUsers(mock.Anything, []uuid.UUID{userID, otherID}).Return([]model.User{user, {ID: otherID, Username: "bob", Email: "bob@example.com", Role: model.RoleUser, Status: model.StatusActive}}, nil)
	conn := newSSOGRPCConn(t, auth, false)
	authClient := ssoapi.NewAuthClient(conn)
	userClient := ssoapi.NewUserAPIClient(conn)

	_, err := userClient.VerifyAccessToken(context.Background(), &ssoapi.VerifyTokenRequest{})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	verifyResp, err := userClient.VerifyAccessToken(context.Background(), &ssoapi.VerifyTokenRequest{AccessToken: "access"})
	require.NoError(t, err)
	requireUserData(t, verifyResp, user)

	permResp, err := userClient.CheckPermission(context.Background(), &ssoapi.CheckPermissionRequest{AccessToken: "access", Permission: "users.admin.list"})
	require.NoError(t, err)
	require.True(t, permResp.Allowed)
	requireUserData(t, permResp.User, user)

	getResp, err := userClient.GetUser(context.Background(), &ssoapi.GetUserRequest{UserId: userID.String()})
	require.NoError(t, err)
	requireUserData(t, getResp, user)

	_, err = userClient.GetUser(context.Background(), &ssoapi.GetUserRequest{UserId: "bad"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	batchResp, err := userClient.BatchGetUsers(context.Background(), &ssoapi.BatchGetUsersRequest{UserIds: []string{userID.String(), userID.String(), otherID.String()}})
	require.NoError(t, err)
	require.Len(t, batchResp.Users, 2)

	_, err = authClient.StartOIDCLogin(context.Background(), &ssoapi.StartOIDCLoginRequest{})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSSOUserGRPCAdminRoutes(t *testing.T) {
	auth := ssogrpcmocks.NewMockAuthService(t)
	userID := uuid.New()
	user := model.User{ID: userID, Username: "alice", Email: "alice@example.com", Role: model.RoleUser, Status: model.StatusActive, QuotaCPU: 1, QuotaRAMMB: 2048, QuotaDiskMB: 5120}
	auth.EXPECT().ListUsers(mock.Anything, 20, 20).Return([]model.User{user}, 1, nil)
	auth.EXPECT().CreateUser(mock.Anything, mock.MatchedBy(func(input ssoservice.CreateUserInput) bool {
		return input.Username == "bob" && input.Role == model.RoleUser && input.QuotaRAMMB == 1024
	})).Return(user, nil)
	auth.EXPECT().UpdateUser(mock.Anything, userID, mock.MatchedBy(func(input ssoservice.UpdateUserInput) bool {
		return input.Username == "alice2" && input.Password == "newsecret"
	})).Return(user, nil)
	auth.EXPECT().DeactivateUser(mock.Anything, userID).Return(user, nil)
	auth.EXPECT().ReactivateUser(mock.Anything, userID).Return(user, nil)
	conn := newSSOGRPCConn(t, auth, true)
	client := ssoapi.NewUserAPIClient(conn)

	listResp, err := client.ListUsers(context.Background(), &ssoapi.ListUsersRequest{Page: 2})
	require.NoError(t, err)
	require.Equal(t, int32(1), listResp.TotalCount)
	require.Len(t, listResp.Users, 1)

	createResp, err := client.CreateUser(context.Background(), &ssoapi.CreateUserRequest{Username: "bob", Email: "bob@example.com", Password: "secret123", Role: model.RoleUser, Status: model.StatusActive, QuotaCpu: 1, QuotaRamMb: 1024, QuotaDiskMb: 2048})
	require.NoError(t, err)
	requireUserData(t, createResp, user)

	updateResp, err := client.UpdateUser(context.Background(), &ssoapi.UpdateUserRequest{UserId: userID.String(), Username: "alice2", Email: "alice2@example.com", Password: "newsecret", Role: model.RoleUser, Status: model.StatusActive, QuotaCpu: 2, QuotaRamMb: 4096, QuotaDiskMb: 8192})
	require.NoError(t, err)
	requireUserData(t, updateResp, user)

	_, err = client.DeactivateUser(context.Background(), &ssoapi.UserIDRequest{UserId: userID.String()})
	require.NoError(t, err)
	_, err = client.ReactivateUser(context.Background(), &ssoapi.UserIDRequest{UserId: userID.String()})
	require.NoError(t, err)

	_, err = client.UpdateUser(context.Background(), &ssoapi.UpdateUserRequest{UserId: "bad"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSSOUserGRPCRejectsAdminRoutesWithoutAdminScope(t *testing.T) {
	conn := newSSOGRPCConn(t, ssogrpcmocks.NewMockAuthService(t), false)
	client := ssoapi.NewUserAPIClient(conn)

	_, err := client.ListUsers(context.Background(), &ssoapi.ListUsersRequest{})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func newSSOGRPCConn(t *testing.T, auth ssogrpc.AuthService, injectAdmin bool) *grpc.ClientConn {
	t.Helper()
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

func requireUserData(t *testing.T, got *ssoapi.UserData, want model.User) {
	t.Helper()
	require.Equal(t, want.ID.String(), got.UserId)
	require.Equal(t, want.Username, got.Username)
	require.Equal(t, want.Email, got.Email)
	require.Equal(t, want.Role, got.Role)
	require.Equal(t, want.Status, got.Status)
	require.Equal(t, want.QuotaCPU, got.QuotaCpu)
	require.Equal(t, want.QuotaRAMMB, got.QuotaRamMb)
	require.Equal(t, want.QuotaDiskMB, got.QuotaDiskMb)
}
