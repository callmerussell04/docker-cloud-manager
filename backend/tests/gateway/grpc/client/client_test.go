package client_test

import (
	"context"
	"net"
	"testing"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	ssoapi "github.com/callmerussell04/docker-cloud-manager/api/sso"
	grpcclient "github.com/callmerussell04/docker-cloud-manager/internal/gateway/grpc/client"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestSSOClientMapsAuthOIDCAndUserManagementRPCs(t *testing.T) {
	servers := &ssoServers{auth: &ssoAuthServer{}, users: &ssoUserServer{}}
	conn := newGRPCConn(t, func(server *grpc.Server) {
		ssoapi.RegisterAuthServer(server, servers.auth)
		ssoapi.RegisterUserAPIServer(server, servers.users)
	})
	client := grpcclient.NewSSOClient(conn)

	userID, err := client.Register(context.Background(), "alice", "alice@example.com", "secret")
	require.NoError(t, err)
	require.Equal(t, "user-id", userID)
	require.Equal(t, "alice@example.com", servers.auth.registerEmail)

	tokens, err := client.Login(context.Background(), "alice", "secret")
	require.NoError(t, err)
	require.Equal(t, model.Tokens{AccessToken: "access", RefreshToken: "refresh"}, tokens)

	config, err := client.GetAuthConfig(context.Background())
	require.NoError(t, err)
	require.True(t, config.LocalLoginEnabled)
	require.Equal(t, []model.OIDCProvider{{Name: "keycloak"}}, config.OIDCProviders)

	start, err := client.StartOIDCLogin(context.Background(), "keycloak", "http://frontend/auth/callback")
	require.NoError(t, err)
	require.Equal(t, "https://idp/auth", start.AuthURL)
	require.Equal(t, "binding", start.StateBinding)

	callback, err := client.CompleteOIDCCallback(context.Background(), "keycloak", "code", "state", "binding")
	require.NoError(t, err)
	require.Equal(t, "http://frontend/auth/callback", callback.RedirectAfter)
	require.Equal(t, "oidc-access", callback.Tokens.AccessToken)

	user, err := client.VerifyAccessToken(context.Background(), "Bearer access")
	require.NoError(t, err)
	require.Equal(t, model.AuthUser{UserID: "user-id", Username: "alice", Role: "user"}, user)
	require.Equal(t, "access", servers.users.verifiedToken)

	admin, err := client.CreateUser(context.Background(), model.CreateUserInput{Username: "bob", Email: "bob@example.com", Password: "secret", Role: "admin", Status: "active", QuotaCPU: 2, QuotaRAMMB: 1024, QuotaDiskMB: 2048})
	require.NoError(t, err)
	require.Equal(t, "bob", admin.Username)
	require.Equal(t, float64(2), servers.users.createdQuotaCPU)

	list, err := client.ListUsers(context.Background(), 2, 10)
	require.NoError(t, err)
	require.Equal(t, int32(1), list.TotalCount)
	require.Equal(t, "alice", list.Users[0].Username)
}

func TestSSOClientAuthHeaderPermissionAndGRPCErrorMapping(t *testing.T) {
	servers := &ssoServers{auth: &ssoAuthServer{}, users: &ssoUserServer{}}
	conn := newGRPCConn(t, func(server *grpc.Server) {
		ssoapi.RegisterAuthServer(server, servers.auth)
		ssoapi.RegisterUserAPIServer(server, servers.users)
	})
	client := grpcclient.NewSSOClient(conn)

	_, err := client.VerifyAccessToken(context.Background(), "Token access")
	require.ErrorIs(t, err, apperrors.ErrUnauthorized)

	servers.users.allowPermission = true
	user, err := client.CheckPermission(context.Background(), "Bearer access", "system.config.read")
	require.NoError(t, err)
	require.Equal(t, "admin", user.Role)
	require.Equal(t, "system.config.read", servers.users.permission)

	servers.users.allowPermission = false
	_, err = client.CheckPermission(context.Background(), "Bearer access", "system.config.read")
	require.ErrorIs(t, err, apperrors.ErrForbidden)

	servers.auth.loginErr = status.Error(codes.NotFound, apperrors.ErrNotFound.Error())
	_, err = client.Login(context.Background(), "missing", "secret")
	require.ErrorIs(t, err, apperrors.ErrNotFound)
}

func TestCoreClientMapsContainersStatsSystemAndReports(t *testing.T) {
	containerSrv := &coreContainerServer{}
	systemSrv := &coreSystemServer{}
	statsSrv := &coreStatsServer{}
	reportSrv := &coreReportServer{}
	conn := newGRPCConn(t, func(server *grpc.Server) {
		coreapi.RegisterContainerAPIServer(server, containerSrv)
		coreapi.RegisterSystemAPIServer(server, systemSrv)
		coreapi.RegisterStatsAPIServer(server, statsSrv)
		coreapi.RegisterReportAPIServer(server, reportSrv)
	})
	client := grpcclient.NewCoreClient(conn)

	containerID, err := client.CreateContainer(context.Background(), model.CreateContainerInput{
		Name:         "web",
		ImageTag:     "nginx:latest",
		InternalPort: 80,
		DomainPrefix: "web",
		EnvVars:      map[string]string{"APP_ENV": "test"},
		VolumeMounts: []model.VolumeMountInput{{VolumeID: "volume-id", MountPath: "/data", IsReadOnly: true}},
	})
	require.NoError(t, err)
	require.Equal(t, "container-id", containerID)
	require.Equal(t, "nginx:latest", containerSrv.create.ImageTag)
	require.True(t, containerSrv.create.VolumeMounts[0].IsReadonly)

	require.NoError(t, client.ActionContainer(context.Background(), "container-id", "start"))
	require.Equal(t, "container-id", containerSrv.startedID)
	require.ErrorIs(t, client.ActionContainer(context.Background(), "container-id", "restart"), apperrors.ErrBadRequest)

	containers, err := client.ListContainers(context.Background(), 2, 10, "project-id")
	require.NoError(t, err)
	require.Equal(t, int32(1), containers.TotalCount)
	require.Equal(t, "docker-id", containers.Containers[0].DockerID)
	require.Equal(t, "project-id", containers.Containers[0].ProjectID)
	require.NotNil(t, containers.Containers[0].LastExitCode)
	require.Equal(t, 137, *containers.Containers[0].LastExitCode)
	require.Equal(t, int32(2), containerSrv.page)
	require.Equal(t, int32(10), containerSrv.limit)
	require.Equal(t, "project-id", containerSrv.projectID)

	stats, err := client.GetContainerStats(context.Background(), "container-id")
	require.NoError(t, err)
	require.Equal(t, 12.5, stats.CPUPercentage)

	config, err := client.GetSystemConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, "example.test", config.BaseDomain)
	require.True(t, config.ImageBuildsEnabled)
	require.EqualValues(t, 250, config.DefaultCPUReservationMillicores)
	require.NoError(t, client.UpdateSystemConfig(context.Background(), model.SystemConfig{BaseDomain: "updated.test", MaxContainersPerUser: 5, DefaultCPUReservationMillicores: 500}))
	require.Equal(t, "updated.test", systemSrv.updated.BaseDomain)
	require.Equal(t, int32(5), systemSrv.updated.MaxContainersPerUser)
	require.EqualValues(t, 500, systemSrv.updated.DefaultCpuReservationMillicores)

	userStats, err := client.GetUserStats(context.Background())
	require.NoError(t, err)
	require.Equal(t, int32(3), userStats.ContainersTotal)
	monitoring, err := client.GetSystemMonitoring(context.Background())
	require.NoError(t, err)
	require.Equal(t, 42.5, monitoring.CPUPercent)

	overview, err := client.GetReportsOverview(context.Background(), 1, 2)
	require.NoError(t, err)
	require.Equal(t, int64(7), overview.AuditEventsTotal)
	require.Equal(t, []model.ActionCount{{Action: "containers.create", Count: 2}}, overview.TopActions)
	refresh, err := client.RefreshUsageSnapshots(context.Background())
	require.NoError(t, err)
	require.Equal(t, 3, refresh.SnapshotsCount)
	require.NoError(t, client.RecordAuditEvent(context.Background(), model.AuditEvent{Action: "containers.create", ActorUserID: "user-id"}))
	require.Equal(t, "containers.create", reportSrv.recorded.Action)
}

func TestCoreClientMapsGRPCErrorsToAppErrors(t *testing.T) {
	conn := newGRPCConn(t, func(server *grpc.Server) {
		coreapi.RegisterContainerAPIServer(server, &coreContainerServer{createErr: status.Error(codes.Unavailable, apperrors.ErrUnavailable.Error())})
	})
	client := grpcclient.NewCoreClient(conn)

	_, err := client.CreateContainer(context.Background(), model.CreateContainerInput{Name: "web"})
	require.ErrorIs(t, err, apperrors.ErrUnavailable)
}

func newGRPCConn(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	register(server)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

type ssoServers struct {
	auth  *ssoAuthServer
	users *ssoUserServer
}

type ssoAuthServer struct {
	ssoapi.UnimplementedAuthServer
	registerEmail string
	loginErr      error
}

func (s *ssoAuthServer) Register(_ context.Context, req *ssoapi.RegisterRequest) (*ssoapi.RegisterResponse, error) {
	s.registerEmail = req.Email
	return &ssoapi.RegisterResponse{UserId: "user-id"}, nil
}

func (s *ssoAuthServer) Login(context.Context, *ssoapi.LoginRequest) (*ssoapi.LoginResponse, error) {
	if s.loginErr != nil {
		return nil, s.loginErr
	}
	return &ssoapi.LoginResponse{AccessToken: "access", RefreshToken: "refresh"}, nil
}

func (s *ssoAuthServer) Refresh(context.Context, *ssoapi.RefreshRequest) (*ssoapi.RefreshResponse, error) {
	return &ssoapi.RefreshResponse{AccessToken: "access2", RefreshToken: "refresh2"}, nil
}

func (s *ssoAuthServer) GetAuthConfig(context.Context, *ssoapi.GetAuthConfigRequest) (*ssoapi.GetAuthConfigResponse, error) {
	return &ssoapi.GetAuthConfigResponse{
		LocalLoginEnabled:    true,
		LocalRegisterEnabled: false,
		OidcProviders:        []*ssoapi.OIDCProviderData{{Name: "keycloak"}},
	}, nil
}

func (s *ssoAuthServer) StartOIDCLogin(context.Context, *ssoapi.StartOIDCLoginRequest) (*ssoapi.StartOIDCLoginResponse, error) {
	return &ssoapi.StartOIDCLoginResponse{AuthUrl: "https://idp/auth", StateBinding: "binding"}, nil
}

func (s *ssoAuthServer) CompleteOIDCCallback(context.Context, *ssoapi.CompleteOIDCCallbackRequest) (*ssoapi.CompleteOIDCCallbackResponse, error) {
	return &ssoapi.CompleteOIDCCallbackResponse{AccessToken: "oidc-access", RefreshToken: "oidc-refresh", RedirectAfter: "http://frontend/auth/callback"}, nil
}

type ssoUserServer struct {
	ssoapi.UnimplementedUserAPIServer
	verifiedToken     string
	permission        string
	allowPermission   bool
	createdQuotaCPU   float64
	updatedStatus     string
	deactivatedUserID string
	reactivatedUserID string
}

func (s *ssoUserServer) VerifyAccessToken(_ context.Context, req *ssoapi.VerifyTokenRequest) (*ssoapi.UserData, error) {
	s.verifiedToken = req.AccessToken
	return &ssoapi.UserData{UserId: "user-id", Username: "alice", Role: "user"}, nil
}

func (s *ssoUserServer) CheckPermission(_ context.Context, req *ssoapi.CheckPermissionRequest) (*ssoapi.CheckPermissionResponse, error) {
	s.permission = req.Permission
	return &ssoapi.CheckPermissionResponse{
		Allowed: s.allowPermission,
		User:    &ssoapi.UserData{UserId: "admin-id", Username: "admin", Role: "admin"},
	}, nil
}

func (s *ssoUserServer) ListUsers(_ context.Context, req *ssoapi.ListUsersRequest) (*ssoapi.ListUsersResponse, error) {
	return &ssoapi.ListUsersResponse{
		Users:      []*ssoapi.UserData{{UserId: "user-id", Username: "alice", Email: "alice@example.com", Role: "user", Status: "active", QuotaCpu: 1, QuotaRamMb: 1024, QuotaDiskMb: 2048}},
		TotalCount: 1,
	}, nil
}

func (s *ssoUserServer) GetUser(context.Context, *ssoapi.GetUserRequest) (*ssoapi.UserData, error) {
	return &ssoapi.UserData{UserId: "user-id", Username: "alice"}, nil
}

func (s *ssoUserServer) BatchGetUsers(context.Context, *ssoapi.BatchGetUsersRequest) (*ssoapi.BatchGetUsersResponse, error) {
	return &ssoapi.BatchGetUsersResponse{}, nil
}

func (s *ssoUserServer) CreateUser(_ context.Context, req *ssoapi.CreateUserRequest) (*ssoapi.UserData, error) {
	s.createdQuotaCPU = req.QuotaCpu
	return &ssoapi.UserData{UserId: "user-id", Username: req.Username, Email: req.Email, Role: req.Role, Status: req.Status, QuotaCpu: req.QuotaCpu, QuotaRamMb: req.QuotaRamMb, QuotaDiskMb: req.QuotaDiskMb}, nil
}

func (s *ssoUserServer) UpdateUser(_ context.Context, req *ssoapi.UpdateUserRequest) (*ssoapi.UserData, error) {
	s.updatedStatus = req.Status
	return &ssoapi.UserData{UserId: req.UserId, Username: req.Username, Status: req.Status}, nil
}

func (s *ssoUserServer) DeactivateUser(_ context.Context, req *ssoapi.UserIDRequest) (*ssoapi.UserData, error) {
	s.deactivatedUserID = req.UserId
	return &ssoapi.UserData{UserId: req.UserId, Status: "inactive"}, nil
}

func (s *ssoUserServer) ReactivateUser(_ context.Context, req *ssoapi.UserIDRequest) (*ssoapi.UserData, error) {
	s.reactivatedUserID = req.UserId
	return &ssoapi.UserData{UserId: req.UserId, Status: "active"}, nil
}

type coreContainerServer struct {
	coreapi.UnimplementedContainerAPIServer
	create    *coreapi.CreateContainerRequest
	createErr error
	startedID string
	page      int32
	limit     int32
	projectID string
}

func (s *coreContainerServer) CreateContainer(_ context.Context, req *coreapi.CreateContainerRequest) (*coreapi.CreateContainerResponse, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	s.create = req
	return &coreapi.CreateContainerResponse{ContainerId: "container-id"}, nil
}

func (s *coreContainerServer) StartContainer(_ context.Context, req *coreapi.ContainerActionRequest) (*coreapi.Empty, error) {
	s.startedID = req.ContainerId
	return &coreapi.Empty{}, nil
}

func (s *coreContainerServer) StopContainer(context.Context, *coreapi.ContainerActionRequest) (*coreapi.Empty, error) {
	return &coreapi.Empty{}, nil
}

func (s *coreContainerServer) DeleteContainer(context.Context, *coreapi.ContainerActionRequest) (*coreapi.Empty, error) {
	return &coreapi.Empty{}, nil
}

func (s *coreContainerServer) ExposeContainer(context.Context, *coreapi.ExposeRequest) (*coreapi.Empty, error) {
	return &coreapi.Empty{}, nil
}

func (s *coreContainerServer) ListContainers(_ context.Context, req *coreapi.PaginationRequest) (*coreapi.PaginatedContainerResponse, error) {
	s.page = req.Page
	s.limit = req.Limit
	s.projectID = req.ProjectId
	exitCode := int32(137)
	return &coreapi.PaginatedContainerResponse{
		Containers: []*coreapi.ContainerData{{Id: "container-id", DockerId: "docker-id", ProjectId: req.ProjectId, Name: "web", LastExitCode: &exitCode, OwnerId: "owner-id", OwnerUsername: "alice"}},
		TotalCount: 1,
	}, nil
}

func (s *coreContainerServer) GetContainerStats(context.Context, *coreapi.ContainerActionRequest) (*coreapi.ContainerStatsResponse, error) {
	return &coreapi.ContainerStatsResponse{CpuPercentage: 12.5, MemoryUsageBytes: 1024}, nil
}

type coreSystemServer struct {
	coreapi.UnimplementedSystemAPIServer
	updated *coreapi.SystemConfigData
}

func (s *coreSystemServer) GetConfig(context.Context, *coreapi.Empty) (*coreapi.SystemConfigData, error) {
	return &coreapi.SystemConfigData{BaseDomain: "example.test", ImageBuildsEnabled: true, DefaultCpuReservationMillicores: 250}, nil
}

func (s *coreSystemServer) UpdateConfig(_ context.Context, req *coreapi.SystemConfigData) (*coreapi.Empty, error) {
	s.updated = req
	return &coreapi.Empty{}, nil
}

type coreStatsServer struct {
	coreapi.UnimplementedStatsAPIServer
}

func (s *coreStatsServer) GetUserStats(context.Context, *coreapi.Empty) (*coreapi.UserStatsResponse, error) {
	return &coreapi.UserStatsResponse{ContainersTotal: 3}, nil
}

func (s *coreStatsServer) GetSystemMonitoring(context.Context, *coreapi.Empty) (*coreapi.SystemMonitoringResponse, error) {
	return &coreapi.SystemMonitoringResponse{CpuPercent: 42.5}, nil
}

type coreReportServer struct {
	coreapi.UnimplementedReportAPIServer
	recorded *coreapi.AuditEventData
}

func (s *coreReportServer) GetReportsOverview(_ context.Context, req *coreapi.ReportRangeRequest) (*coreapi.ReportsOverviewResponse, error) {
	return &coreapi.ReportsOverviewResponse{
		From:             req.From,
		To:               req.To,
		AuditEventsTotal: 7,
		TopActions:       []*coreapi.ActionCountData{{Action: "containers.create", Count: 2}},
	}, nil
}

func (s *coreReportServer) ListUserUsageReport(context.Context, *coreapi.ListUserUsageReportRequest) (*coreapi.PaginatedUserUsageReportResponse, error) {
	return &coreapi.PaginatedUserUsageReportResponse{}, nil
}

func (s *coreReportServer) GetUserUsageTimeline(context.Context, *coreapi.GetUserUsageTimelineRequest) (*coreapi.UserUsageTimelineResponse, error) {
	return &coreapi.UserUsageTimelineResponse{}, nil
}

func (s *coreReportServer) ListAuditEvents(context.Context, *coreapi.ListAuditEventsRequest) (*coreapi.PaginatedAuditEventResponse, error) {
	return &coreapi.PaginatedAuditEventResponse{}, nil
}

func (s *coreReportServer) RecordAuditEvent(_ context.Context, req *coreapi.AuditEventData) (*coreapi.Empty, error) {
	s.recorded = req
	return &coreapi.Empty{}, nil
}

func (s *coreReportServer) RefreshUsageSnapshots(context.Context, *coreapi.Empty) (*coreapi.RefreshUsageSnapshotsResponse, error) {
	return &coreapi.RefreshUsageSnapshotsResponse{SnapshotsCount: 3}, nil
}
