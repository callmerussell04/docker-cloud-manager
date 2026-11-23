package client_test

import (
	"context"
	"net"
	"testing"
	"time"

	coreapi "github.com/callmerussell04/docker-cloud-manager/backend/api/core"
	"github.com/callmerussell04/docker-cloud-manager/backend/api/sso"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/grpc/client"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/grpcerrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/fieldfill"
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
		sso.RegisterAuthServer(server, servers.auth)
		sso.RegisterUserAPIServer(server, servers.users)
	})
	ssoClient := client.NewSSOClient(conn)

	userID, err := ssoClient.Register(context.Background(), "alice", "alice@example.com", "secret")
	require.NoError(t, err)
	require.Equal(t, "user-id", userID)
	require.Equal(t, "alice@example.com", servers.auth.registerEmail)

	signIn, err := ssoClient.Login(context.Background(), "alice", "secret")
	require.NoError(t, err)
	require.Equal(t, model.SignIn{
		Tokens: model.Tokens{AccessToken: "access", RefreshToken: "refresh", RefreshExpiresAt: time.Unix(0, 0)},
		User:   model.SignedInUser{ID: "user-id", Username: "alice", Role: "admin"},
	}, signIn)

	config, err := ssoClient.GetAuthConfig(context.Background())
	require.NoError(t, err)
	require.True(t, config.LocalLoginEnabled)
	require.Equal(t, []model.OIDCProvider{{Name: "keycloak", Available: true}}, config.OIDCProviders)

	start, err := ssoClient.StartOIDCLogin(context.Background(), "keycloak", "http://frontend/auth/callback")
	require.NoError(t, err)
	require.Equal(t, "https://idp/auth", start.AuthURL)
	require.Equal(t, "binding", start.StateBinding)
	require.Equal(t, "state", start.State)
	require.Equal(t, time.Unix(2000000000, 0), start.ExpiresAt)

	callback, err := ssoClient.CompleteOIDCCallback(context.Background(), "keycloak", "code", "state", "binding")
	require.NoError(t, err)
	require.Equal(t, "http://frontend/auth/callback", callback.RedirectAfter)
	require.Equal(t, model.OIDCCallbackSuccess, callback.Outcome)
	require.Equal(t, "oidc-access", callback.Tokens.AccessToken)
	require.Equal(t, model.SignedInUser{ID: "oidc-user-id", Username: "bob", Role: "user"}, callback.User)

	signOutURL, err := ssoClient.Logout(context.Background(), "refresh", "http://frontend/login")
	require.NoError(t, err)
	require.Equal(t, "https://idp/logout", signOutURL)
	require.Equal(t, "http://frontend/login", servers.auth.postLogoutRedirectURI)

	revoked, err := ssoClient.BackchannelLogout(context.Background(), "keycloak", "logout-token")
	require.NoError(t, err)
	require.Equal(t, 3, revoked)
	require.Equal(t, "logout-token", servers.auth.logoutToken)

	user, err := ssoClient.VerifyAccessToken(context.Background(), "Bearer access")
	require.NoError(t, err)
	require.Equal(t, model.AuthUser{UserID: "user-id", Username: "alice", Role: "user"}, user)
	require.Equal(t, "access", servers.users.verifiedToken)

	admin, err := ssoClient.CreateUser(context.Background(), model.CreateUserInput{Username: "bob", Email: "bob@example.com", Password: "secret", Role: "admin", Status: "active"})
	require.NoError(t, err)
	require.Equal(t, "bob", admin.Username)
	require.Equal(t, "admin", servers.users.createdRole)

	list, err := ssoClient.ListUsers(context.Background(), 2, 10)
	require.NoError(t, err)
	require.Equal(t, int32(1), list.TotalCount)
	require.Equal(t, model.AdminUser{UserID: "user-id", Username: "alice", Email: "alice@example.com", Role: "user", Status: "active", AuthSource: "oidc", RoleManagedByProvider: true}, list.Users[0])
}

func TestSSOClientAuthHeaderPermissionAndGRPCErrorMapping(t *testing.T) {
	servers := &ssoServers{auth: &ssoAuthServer{}, users: &ssoUserServer{}}
	conn := newGRPCConn(t, func(server *grpc.Server) {
		sso.RegisterAuthServer(server, servers.auth)
		sso.RegisterUserAPIServer(server, servers.users)
	})
	ssoClient := client.NewSSOClient(conn)

	_, err := ssoClient.VerifyAccessToken(context.Background(), "Token access")
	require.ErrorIs(t, err, apperrors.ErrUnauthorized)

	servers.users.allowPermission = true
	user, err := ssoClient.CheckPermission(context.Background(), "Bearer access", "system.config.read")
	require.NoError(t, err)
	require.Equal(t, "admin", user.Role)
	require.Equal(t, "system.config.read", servers.users.permission)

	servers.users.allowPermission = false
	_, err = ssoClient.CheckPermission(context.Background(), "Bearer access", "system.config.read")
	require.ErrorIs(t, err, apperrors.ErrForbidden)

	servers.auth.loginErr = status.Error(codes.NotFound, apperrors.ErrNotFound.Error())
	_, err = ssoClient.Login(context.Background(), "missing", "secret")
	require.ErrorIs(t, err, apperrors.ErrNotFound)
}

func TestCoreClientMapsVolumeSizes(t *testing.T) {
	volumeSrv := &coreVolumeServer{}
	conn := newGRPCConn(t, func(server *grpc.Server) {
		coreapi.RegisterVolumeAPIServer(server, volumeSrv)
	})
	coreClient := client.NewCoreClient(conn)

	id, err := coreClient.CreateVolume(context.Background(), model.CreateVolumeInput{Name: "data", SizeBytes: 32 << 20})
	require.NoError(t, err)
	require.Equal(t, "volume-id", id)
	require.Equal(t, "data", volumeSrv.created.GetName())
	require.EqualValues(t, 32<<20, volumeSrv.created.GetSizeBytes())

	volumes, err := coreClient.ListVolumes(context.Background(), 1, 10)
	require.NoError(t, err)
	require.Len(t, volumes.Volumes, 2)
	require.EqualValues(t, 1<<30, volumes.Volumes[0].SizeBytes)
	require.False(t, volumes.Volumes[0].SizeEnforced)
	require.True(t, volumes.Volumes[1].SizeEnforced)
	require.Equal(t, []model.VolumeUser{{ContainerID: "container-id", Name: "web"}}, volumes.Volumes[0].UsedBy)
	require.Empty(t, volumes.Volumes[1].UsedBy)
}

func TestCoreClientMapsGetProject(t *testing.T) {
	projectSrv := &coreProjectServer{}
	conn := newGRPCConn(t, func(server *grpc.Server) {
		coreapi.RegisterProjectAPIServer(server, projectSrv)
	})
	coreClient := client.NewCoreClient(conn)

	project, err := coreClient.GetProject(context.Background(), "project-id")

	require.NoError(t, err)
	require.Equal(t, "project-id", projectSrv.gotID)
	require.Equal(t, model.Project{
		ID: "project-id", Name: "shop", Status: "failed", ErrorMessage: "build failed", LastError: "build failed",
		CreatedAt: 100, OwnerID: "owner-id", OwnerUsername: "alice",
	}, project)

	projectSrv.err = status.Error(codes.NotFound, apperrors.ErrNotFound.Error())
	_, err = coreClient.GetProject(context.Background(), "project-id")
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
	coreClient := client.NewCoreClient(conn)

	containerID, err := coreClient.CreateContainer(context.Background(), model.CreateContainerInput{
		Name:         "web",
		ImageTag:     "nginx:latest",
		InternalPort: 80,
		DomainPrefix: "web",
		EnvVars:      map[string]string{"APP_ENV": "test"},
		VolumeMounts: []model.VolumeMountInput{{VolumeID: "volume-id", MountPath: "/data", IsReadOnly: true}},

		MemoryReservationMB:      512,
		CPUReservationMillicores: 750,
	})
	require.NoError(t, err)
	require.Equal(t, "container-id", containerID)
	require.EqualValues(t, 512, containerSrv.create.GetMemoryReservationMb())
	require.EqualValues(t, 750, containerSrv.create.GetCpuReservationMillicores())
	require.Equal(t, "nginx:latest", containerSrv.create.ImageTag)
	require.True(t, containerSrv.create.VolumeMounts[0].IsReadonly)

	require.NoError(t, coreClient.ActionContainer(context.Background(), "container-id", "start"))
	require.Equal(t, "container-id", containerSrv.startedID)
	require.ErrorIs(t, coreClient.ActionContainer(context.Background(), "container-id", "restart"), apperrors.ErrBadRequest)

	containers, err := coreClient.ListContainers(context.Background(), 2, 10, "project-id")
	require.NoError(t, err)
	require.Equal(t, int32(1), containers.TotalCount)
	require.Equal(t, "docker-id", containers.Containers[0].DockerID)
	require.Equal(t, "project-id", containers.Containers[0].ProjectID)
	require.NotNil(t, containers.Containers[0].LastExitCode)
	require.Equal(t, 137, *containers.Containers[0].LastExitCode)
	require.EqualValues(t, 512<<20, containers.Containers[0].MemoryReservationBytes)
	require.EqualValues(t, 750, containers.Containers[0].CPUReservationMillicores)
	require.Equal(t, int32(2), containerSrv.page)
	require.Equal(t, int32(10), containerSrv.limit)
	require.Equal(t, "project-id", containerSrv.projectID)

	container, err := coreClient.GetContainer(context.Background(), "container-id")
	require.NoError(t, err)
	require.Equal(t, "container-id", containerSrv.gotID)
	require.Equal(t, "docker-id", container.DockerID)
	require.Equal(t, "alice", container.OwnerUsername)
	require.NotNil(t, container.LastExitCode)
	require.Equal(t, 137, *container.LastExitCode)

	stats, err := coreClient.GetContainerStats(context.Background(), "container-id")
	require.NoError(t, err)
	require.Equal(t, 12.5, stats.CPUPercentage)
	require.EqualValues(t, 3072, stats.MemoryLimitBytes)
	require.EqualValues(t, 2048, stats.MemoryStandardLimitBytes)

	config, err := coreClient.GetSystemConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, "25m", config.MaxLogSize)
	require.True(t, config.ImageBuildsEnabled)
	require.EqualValues(t, 250, config.DefaultCPUReservationMillicores)
	require.EqualValues(t, 1<<30, config.DefaultVolumeSizeBytes)
	require.True(t, config.VolumeSizeEnforcement)
	require.NoError(t, coreClient.UpdateSystemConfig(context.Background(), model.SystemConfig{MaxLogSize: "50m", MaxContainersPerUser: 5, DefaultCPUReservationMillicores: 500, DefaultVolumeSizeBytes: 2 << 30, VolumeSizeEnforcement: true}))
	require.Equal(t, "50m", systemSrv.updated.MaxLogSize)
	require.Equal(t, int32(5), systemSrv.updated.MaxContainersPerUser)
	require.EqualValues(t, 500, systemSrv.updated.DefaultCpuReservationMillicores)
	require.EqualValues(t, 2<<30, systemSrv.updated.DefaultVolumeSizeBytes)
	require.True(t, systemSrv.updated.GetVolumeSizeEnforcement())

	userStats, err := coreClient.GetUserStats(context.Background())
	require.NoError(t, err)
	require.Equal(t, int32(3), userStats.ContainersTotal)
	require.EqualValues(t, 750, userStats.CPUUsedMillicores)
	require.EqualValues(t, 2000, userStats.CPUQuotaMillicores)
	monitoring, err := coreClient.GetSystemMonitoring(context.Background())
	require.NoError(t, err)
	require.Equal(t, 42.5, monitoring.CPUPercent)
	require.EqualValues(t, 512, monitoring.DCMBorrowedMemoryBytes)
	require.EqualValues(t, 750, monitoring.DCMReservedCPUMillicores)
	require.EqualValues(t, 500, monitoring.DCMReservedBuildCPUMillicores)
	require.Equal(t, "not_enforced", monitoring.RootfsQuotaStatus)
	require.EqualValues(t, 4096, monitoring.DCMReservedVolumeBytes)
	require.EqualValues(t, 1024, monitoring.DCMUnusedVolumePromiseBytes)
	require.Equal(t, 1, monitoring.BuildClaimCap)
	require.Equal(t, 2, monitoring.MaxConcurrentBuilds)
	require.Equal(t, "enforced", monitoring.VolumeSizeMode)

	overview, err := coreClient.GetReportsOverview(context.Background(), 1, 2)
	require.NoError(t, err)
	require.Equal(t, int64(7), overview.AuditEventsTotal)
	require.Equal(t, []model.ActionCount{{Action: "containers.create", Count: 2}}, overview.TopActions)
	refresh, err := coreClient.RefreshUsageSnapshots(context.Background())
	require.NoError(t, err)
	require.Equal(t, 3, refresh.SnapshotsCount)
	require.NoError(t, coreClient.RecordAuditEvent(context.Background(), model.AuditEvent{Action: "containers.create", ActorUserID: "user-id"}))
	require.Equal(t, "containers.create", reportSrv.recorded.Action)
}

func TestCoreClientMapsGRPCErrorsToAppErrors(t *testing.T) {
	conn := newGRPCConn(t, func(server *grpc.Server) {
		coreapi.RegisterContainerAPIServer(server, &coreContainerServer{createErr: status.Error(codes.Unavailable, apperrors.ErrUnavailable.Error())})
	})
	coreClient := client.NewCoreClient(conn)

	_, err := coreClient.CreateContainer(context.Background(), model.CreateContainerInput{Name: "web"})
	require.ErrorIs(t, err, apperrors.ErrUnavailable)
}

// A rejection Core names by its limit reaches Gateway with its kind, its
// message and the limit name.
func TestCoreClientKeepsTheLimitCoreNamed(t *testing.T) {
	conn := newGRPCConn(t, func(server *grpc.Server) {
		coreapi.RegisterContainerAPIServer(server, &coreContainerServer{createErr: grpcerrors.ToGRPC(apperrors.Exceeded(apperrors.LimitUserCPU))})
	})
	coreClient := client.NewCoreClient(conn)

	_, err := coreClient.CreateContainer(context.Background(), model.CreateContainerInput{Name: "web"})

	require.ErrorIs(t, err, apperrors.ErrQuotaExceeded)
	require.Equal(t, apperrors.LimitUserCPU, apperrors.LimitOf(err))
	require.Equal(t, "user CPU quota exceeded", apperrors.SafeMessage(err))
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
	sso.UnimplementedAuthServer
	registerEmail         string
	loginErr              error
	postLogoutRedirectURI string
	logoutToken           string
}

func (s *ssoAuthServer) Logout(_ context.Context, req *sso.LogoutRequest) (*sso.LogoutResponse, error) {
	s.postLogoutRedirectURI = req.GetPostLogoutRedirectUri()
	return &sso.LogoutResponse{ProviderLogoutUrl: "https://idp/logout"}, nil
}

func (s *ssoAuthServer) BackchannelLogout(_ context.Context, req *sso.BackchannelLogoutRequest) (*sso.BackchannelLogoutResponse, error) {
	s.logoutToken = req.GetLogoutToken()
	return &sso.BackchannelLogoutResponse{RevokedSessions: 3}, nil
}

func (s *ssoAuthServer) Register(_ context.Context, req *sso.RegisterRequest) (*sso.RegisterResponse, error) {
	s.registerEmail = req.Email
	return &sso.RegisterResponse{UserId: "user-id"}, nil
}

func (s *ssoAuthServer) Login(context.Context, *sso.LoginRequest) (*sso.LoginResponse, error) {
	if s.loginErr != nil {
		return nil, s.loginErr
	}
	return &sso.LoginResponse{AccessToken: "access", RefreshToken: "refresh", User: &sso.SignedInUser{Id: "user-id", Username: "alice", Role: "admin"}}, nil
}

func (s *ssoAuthServer) Refresh(context.Context, *sso.RefreshRequest) (*sso.RefreshResponse, error) {
	return &sso.RefreshResponse{AccessToken: "access2", RefreshToken: "refresh2"}, nil
}

func (s *ssoAuthServer) GetAuthConfig(context.Context, *sso.GetAuthConfigRequest) (*sso.GetAuthConfigResponse, error) {
	return &sso.GetAuthConfigResponse{
		LocalLoginEnabled:    true,
		LocalRegisterEnabled: false,
		OidcProviders:        []*sso.OIDCProviderData{{Name: "keycloak", Available: true}},
	}, nil
}

func (s *ssoAuthServer) StartOIDCLogin(context.Context, *sso.StartOIDCLoginRequest) (*sso.StartOIDCLoginResponse, error) {
	return &sso.StartOIDCLoginResponse{AuthUrl: "https://idp/auth", StateBinding: "binding", State: "state", ExpiresAt: 2000000000}, nil
}

func (s *ssoAuthServer) CompleteOIDCCallback(context.Context, *sso.CompleteOIDCCallbackRequest) (*sso.CompleteOIDCCallbackResponse, error) {
	return &sso.CompleteOIDCCallbackResponse{Outcome: sso.OIDCCallbackOutcome_OIDC_CALLBACK_OUTCOME_SUCCESS, AccessToken: "oidc-access", RefreshToken: "oidc-refresh", RedirectAfter: "http://frontend/auth/callback", User: &sso.SignedInUser{Id: "oidc-user-id", Username: "bob", Role: "user"}}, nil
}

type ssoUserServer struct {
	sso.UnimplementedUserAPIServer
	verifiedToken     string
	permission        string
	allowPermission   bool
	createdRole       string
	updatedStatus     string
	deactivatedUserID string
	reactivatedUserID string
}

func (s *ssoUserServer) VerifyAccessToken(_ context.Context, req *sso.VerifyTokenRequest) (*sso.UserData, error) {
	s.verifiedToken = req.AccessToken
	return &sso.UserData{UserId: "user-id", Username: "alice", Role: "user"}, nil
}

func (s *ssoUserServer) CheckPermission(_ context.Context, req *sso.CheckPermissionRequest) (*sso.CheckPermissionResponse, error) {
	s.permission = req.Permission
	return &sso.CheckPermissionResponse{
		Allowed: s.allowPermission,
		User:    &sso.UserData{UserId: "admin-id", Username: "admin", Role: "admin"},
	}, nil
}

func (s *ssoUserServer) ListUsers(_ context.Context, req *sso.ListUsersRequest) (*sso.ListUsersResponse, error) {
	return &sso.ListUsersResponse{
		Users:      []*sso.UserData{{UserId: "user-id", Username: "alice", Email: "alice@example.com", Role: "user", Status: "active", AuthSource: "oidc", RoleManagedByProvider: true}},
		TotalCount: 1,
	}, nil
}

func (s *ssoUserServer) GetUser(context.Context, *sso.GetUserRequest) (*sso.UserData, error) {
	return &sso.UserData{UserId: "user-id", Username: "alice"}, nil
}

func (s *ssoUserServer) BatchGetUsers(context.Context, *sso.BatchGetUsersRequest) (*sso.BatchGetUsersResponse, error) {
	return &sso.BatchGetUsersResponse{}, nil
}

func (s *ssoUserServer) CreateUser(_ context.Context, req *sso.CreateUserRequest) (*sso.UserData, error) {
	s.createdRole = req.Role
	return &sso.UserData{UserId: "user-id", Username: req.Username, Email: req.Email, Role: req.Role, Status: req.Status}, nil
}

func (s *ssoUserServer) UpdateUser(_ context.Context, req *sso.UpdateUserRequest) (*sso.UserData, error) {
	s.updatedStatus = req.Status
	return &sso.UserData{UserId: req.UserId, Username: req.Username, Status: req.Status}, nil
}

func (s *ssoUserServer) DeactivateUser(_ context.Context, req *sso.UserIDRequest) (*sso.UserData, error) {
	s.deactivatedUserID = req.UserId
	return &sso.UserData{UserId: req.UserId, Status: "inactive"}, nil
}

func (s *ssoUserServer) ReactivateUser(_ context.Context, req *sso.UserIDRequest) (*sso.UserData, error) {
	s.reactivatedUserID = req.UserId
	return &sso.UserData{UserId: req.UserId, Status: "active"}, nil
}

type coreContainerServer struct {
	coreapi.UnimplementedContainerAPIServer
	create    *coreapi.CreateContainerRequest
	createErr error
	startedID string
	gotID     string
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
		Containers: []*coreapi.ContainerData{{Id: "container-id", DockerId: "docker-id", ProjectId: req.ProjectId, Name: "web", LastExitCode: &exitCode, OwnerId: "owner-id", OwnerUsername: "alice", MemoryReservationBytes: 512 << 20, CpuReservationMillicores: 750}},
		TotalCount: 1,
	}, nil
}

func (s *coreContainerServer) GetContainer(_ context.Context, req *coreapi.ContainerActionRequest) (*coreapi.ContainerData, error) {
	s.gotID = req.GetContainerId()
	exitCode := int32(137)
	return &coreapi.ContainerData{Id: req.GetContainerId(), DockerId: "docker-id", Name: "web", LastExitCode: &exitCode, OwnerId: "owner-id", OwnerUsername: "alice"}, nil
}

func (s *coreContainerServer) GetContainerStats(context.Context, *coreapi.ContainerActionRequest) (*coreapi.ContainerStatsResponse, error) {
	return &coreapi.ContainerStatsResponse{CpuPercentage: 12.5, MemoryUsageBytes: 1024, MemoryLimitBytes: 3072, MemoryStandardLimitBytes: 2048}, nil
}

type coreProjectServer struct {
	coreapi.UnimplementedProjectAPIServer
	gotID string
	err   error
}

func (s *coreProjectServer) GetProject(_ context.Context, req *coreapi.ProjectActionRequest) (*coreapi.ProjectData, error) {
	s.gotID = req.GetProjectId()
	if s.err != nil {
		return nil, s.err
	}
	return &coreapi.ProjectData{
		Id: "project-id", Name: "shop", Status: "failed", ErrorMessage: "build failed", LastError: "build failed",
		CreatedAt: 100, OwnerId: "owner-id", OwnerUsername: "alice",
	}, nil
}

type coreVolumeServer struct {
	coreapi.UnimplementedVolumeAPIServer
	created *coreapi.CreateVolumeRequest
}

func (s *coreVolumeServer) CreateVolume(_ context.Context, req *coreapi.CreateVolumeRequest) (*coreapi.CreateVolumeResponse, error) {
	s.created = req
	return &coreapi.CreateVolumeResponse{VolumeId: "volume-id"}, nil
}

func (s *coreVolumeServer) ListVolumes(context.Context, *coreapi.PaginationRequest) (*coreapi.PaginatedVolumeResponse, error) {
	return &coreapi.PaginatedVolumeResponse{Volumes: []*coreapi.VolumeData{
		{Id: "measured", Name: "data", SizeBytes: 1 << 30, UsedBytes: 10, UsedBy: []*coreapi.VolumeUser{{ContainerId: "container-id", Name: "web"}}},
		{Id: "enforced", Name: "logs", SizeBytes: 2 << 30, SizeEnforced: true},
	}, TotalCount: 2}, nil
}

type coreSystemServer struct {
	coreapi.UnimplementedSystemAPIServer
	// config, when set, is what GetConfig answers.
	config  *coreapi.SystemConfigData
	updated *coreapi.SystemConfigData
}

func (s *coreSystemServer) GetConfig(context.Context, *coreapi.Empty) (*coreapi.SystemConfigData, error) {
	if s.config != nil {
		return s.config, nil
	}
	return &coreapi.SystemConfigData{MaxLogSize: "25m", ImageBuildsEnabled: true, DefaultCpuReservationMillicores: 250, DefaultVolumeSizeBytes: 1 << 30, VolumeSizeEnforcement: true}, nil
}

func (s *coreSystemServer) UpdateConfig(_ context.Context, req *coreapi.SystemConfigData) (*coreapi.Empty, error) {
	s.updated = req
	return &coreapi.Empty{}, nil
}

type coreStatsServer struct {
	coreapi.UnimplementedStatsAPIServer
}

func (s *coreStatsServer) GetUserStats(context.Context, *coreapi.Empty) (*coreapi.UserStatsResponse, error) {
	return &coreapi.UserStatsResponse{ContainersTotal: 3, CpuUsedMillicores: 750, CpuQuotaMillicores: 2000}, nil
}

func (s *coreStatsServer) GetSystemMonitoring(context.Context, *coreapi.Empty) (*coreapi.SystemMonitoringResponse, error) {
	return &coreapi.SystemMonitoringResponse{CpuPercent: 42.5, DcmBorrowedMemoryBytes: 512, DcmReservedCpuMillicores: 750, DcmReservedBuildCpuMillicores: 500, RootfsQuotaStatus: "not_enforced", DcmReservedVolumeBytes: 4096, DcmUnusedVolumePromiseBytes: 1024, BuildClaimCap: 1, MaxConcurrentBuilds: 2, VolumeSizeMode: "enforced"}, nil
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

type coreQuotaServer struct {
	coreapi.UnimplementedQuotaAPIServer
	requestedIDs []string
	set          *coreapi.SetUserQuotaRequest
	resetID      string
}

func (s *coreQuotaServer) GetUserQuotas(_ context.Context, req *coreapi.GetUserQuotasRequest) (*coreapi.UserQuotasResponse, error) {
	s.requestedIDs = req.GetUserIds()
	return &coreapi.UserQuotasResponse{Quotas: []*coreapi.UserQuotaData{
		{UserId: "user-a", Cpu: 2.5, RamMb: 4096, DiskMb: 8192, Custom: true},
		{UserId: "user-b", Cpu: 1, RamMb: 2048, DiskMb: 5120},
	}}, nil
}

func (s *coreQuotaServer) SetUserQuota(_ context.Context, req *coreapi.SetUserQuotaRequest) (*coreapi.UserQuotaData, error) {
	s.set = req
	return &coreapi.UserQuotaData{UserId: req.GetUserId(), Cpu: req.GetCpu(), RamMb: req.GetRamMb(), DiskMb: req.GetDiskMb(), Custom: true}, nil
}

func (s *coreQuotaServer) ResetUserQuota(_ context.Context, req *coreapi.UserQuotaActionRequest) (*coreapi.UserQuotaData, error) {
	s.resetID = req.GetUserId()
	if req.GetUserId() == "missing" {
		return nil, status.Error(codes.NotFound, "not found")
	}
	return &coreapi.UserQuotaData{UserId: req.GetUserId(), Cpu: 1, RamMb: 2048, DiskMb: 5120}, nil
}

func TestCoreClientMapsUserQuotas(t *testing.T) {
	quotaSrv := &coreQuotaServer{}
	conn := newGRPCConn(t, func(server *grpc.Server) {
		coreapi.RegisterQuotaAPIServer(server, quotaSrv)
	})
	coreClient := client.NewCoreClient(conn)
	ctx := context.Background()

	quotas, err := coreClient.GetUserQuotas(ctx, []string{"user-a", "user-b"})
	require.NoError(t, err)
	require.Equal(t, []string{"user-a", "user-b"}, quotaSrv.requestedIDs)
	require.Equal(t, []model.UserQuota{
		{UserID: "user-a", CPU: 2.5, RAMMB: 4096, DiskMB: 8192, Custom: true},
		{UserID: "user-b", CPU: 1, RAMMB: 2048, DiskMB: 5120},
	}, quotas)

	set, err := coreClient.SetUserQuota(ctx, "user-a", model.UserQuotaLimits{CPU: 0.5, RAMMB: 512, DiskMB: 1024})
	require.NoError(t, err)
	require.Equal(t, model.UserQuota{UserID: "user-a", CPU: 0.5, RAMMB: 512, DiskMB: 1024, Custom: true}, set)
	require.Equal(t, int64(512), quotaSrv.set.GetRamMb())

	reset, err := coreClient.ResetUserQuota(ctx, "user-a")
	require.NoError(t, err)
	require.Equal(t, model.UserQuota{UserID: "user-a", CPU: 1, RAMMB: 2048, DiskMB: 5120}, reset)

	_, err = coreClient.ResetUserQuota(ctx, "missing")
	require.ErrorIs(t, err, apperrors.ErrNotFound)
}

// Every runtime setting is mapped between Core's message and Gateway's model
// in both directions.
func TestCoreClientCarriesEveryRuntimeSetting(t *testing.T) {
	systemSrv := &coreSystemServer{}
	conn := newGRPCConn(t, func(server *grpc.Server) {
		coreapi.RegisterSystemAPIServer(server, systemSrv)
	})
	coreClient := client.NewCoreClient(conn)
	var settings model.SystemConfig
	fieldfill.Distinct(t, &settings)

	require.NoError(t, coreClient.UpdateSystemConfig(context.Background(), settings))
	fields := systemSrv.updated.ProtoReflect().Descriptor().Fields()
	for i := range fields.Len() {
		require.Truef(t, systemSrv.updated.ProtoReflect().Has(fields.Get(i)), "an update leaves %s unset", fields.Get(i).Name())
	}

	systemSrv.config = systemSrv.updated
	got, err := coreClient.GetSystemConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, settings, got)
}
