package grpc_test

import (
	"context"
	"net"
	"testing"
	"time"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	coregrpc "github.com/callmerussell04/docker-cloud-manager/internal/core/grpc"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	coregrpcmocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/grpc"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/coretest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestContainerGRPCHandlerMapsCreateListAndRuntimeTarget(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	logic := coregrpcmocks.NewMockContainerLogic(t)
	users := coregrpcmocks.NewMockUserDirectory(t)
	var createParams model.ContainerCreateParams
	logic.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("model.ContainerCreateParams")).
		Run(func(ctx context.Context, params model.ContainerCreateParams) {
			createParams = params
		}).
		Return(containerID, nil)
	logic.EXPECT().List(mock.Anything, 50, 50).Return([]model.Container{{
		ID:           containerID,
		OwnerID:      ownerID,
		Name:         "web",
		ImageTag:     "nginx:latest",
		InternalPort: 8080,
		Status:       model.ContainerStatusRunning,
		CreatedAt:    time.Unix(123, 0),
	}}, 1, nil)
	users.EXPECT().GetUsers(mock.Anything, []uuid.UUID{ownerID}).Return(map[uuid.UUID]model.UserInfo{ownerID: {ID: ownerID, Username: "alice"}}, nil)
	logic.EXPECT().GetRuntimeTarget(mock.Anything, containerID).Return(model.ContainerRuntimeTarget{
		ContainerID:      containerID,
		DockerID:         "docker-id",
		Status:           model.ContainerStatusRunning,
		OwnerID:          ownerID,
		DockerGeneration: 2,
	}, nil)
	conn := newCoreGRPCConn(t, func(s *grpc.Server) {
		coregrpc.RegisterContainerAPI(s, logic, users)
	})
	client := coreapi.NewContainerAPIClient(conn)

	createResp, err := client.CreateContainer(context.Background(), &coreapi.CreateContainerRequest{
		Name: "web", ImageTag: "nginx:latest",
		VolumeMounts: []*coreapi.VolumeMount{{VolumeId: uuid.NewString(), MountPath: "/data", IsReadonly: true}},
	})
	require.NoError(t, err)
	require.Equal(t, containerID.String(), createResp.ContainerId)
	require.Equal(t, "web", createParams.Name)
	require.Len(t, createParams.VolumeMounts, 1)
	require.True(t, createParams.VolumeMounts[0].IsReadOnly)

	listResp, err := client.ListContainers(context.Background(), &coreapi.PaginationRequest{Limit: 50, Page: 2})
	require.NoError(t, err)
	require.Len(t, listResp.Containers, 1)
	require.Equal(t, "alice", listResp.Containers[0].OwnerUsername)
	require.Equal(t, int32(1), listResp.TotalCount)

	targetResp, err := client.GetContainerRuntimeTarget(context.Background(), &coreapi.ContainerRuntimeTargetRequest{ContainerId: containerID.String()})
	require.NoError(t, err)
	require.Equal(t, "docker-id", targetResp.DockerId)
	require.Equal(t, int32(2), targetResp.DockerGeneration)
}

func TestContainerGRPCHandlerRejectsInvalidInputs(t *testing.T) {
	conn := newCoreGRPCConn(t, func(s *grpc.Server) {
		coregrpc.RegisterContainerAPI(s, coregrpcmocks.NewMockContainerLogic(t), nil)
	})
	client := coreapi.NewContainerAPIClient(conn)

	_, err := client.CreateContainer(context.Background(), &coreapi.CreateContainerRequest{Name: "", ImageTag: ""})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = client.StartContainer(context.Background(), &coreapi.ContainerActionRequest{ContainerId: "bad"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = client.ExposeContainer(context.Background(), &coreapi.ExposeRequest{ContainerId: uuid.NewString(), DomainPrefix: "", InternalPort: 0})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestContainerGRPCHandlerPreservesAlreadyExistsMessage(t *testing.T) {
	containerID := uuid.New()
	logic := coregrpcmocks.NewMockContainerLogic(t)
	logic.EXPECT().
		Expose(mock.Anything, containerID, "app", 8080).
		Return(apperrors.New(apperrors.ErrAlreadyExists, "subdomain app.example.test is already in use"))
	conn := newCoreGRPCConn(t, func(s *grpc.Server) {
		coregrpc.RegisterContainerAPI(s, logic, nil)
	})
	client := coreapi.NewContainerAPIClient(conn)

	_, err := client.ExposeContainer(context.Background(), &coreapi.ExposeRequest{
		ContainerId:  containerID.String(),
		DomainPrefix: "app",
		InternalPort: 8080,
	})
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	require.Equal(t, "subdomain app.example.test is already in use", status.Convert(err).Message())
}

func TestVolumeGRPCHandlerMapsCreateListDelete(t *testing.T) {
	ownerID := uuid.New()
	volumeID := uuid.New()
	logic := coregrpcmocks.NewMockVolumeLogic(t)
	users := coregrpcmocks.NewMockUserDirectory(t)
	var createParams model.VolumeCreateParams
	logic.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("model.VolumeCreateParams")).
		Run(func(ctx context.Context, params model.VolumeCreateParams) {
			createParams = params
		}).
		Return(volumeID, nil)
	logic.EXPECT().List(mock.Anything, 10, 0).Return([]model.Volume{{
		ID:         volumeID,
		OwnerID:    ownerID,
		DockerName: "vol_data",
		Status:     model.VolumeStatusAvailable,
		CreatedAt:  time.Unix(321, 0),
		UsedBytes:  128,
	}}, 1, nil)
	users.EXPECT().GetUsers(mock.Anything, []uuid.UUID{ownerID}).Return(map[uuid.UUID]model.UserInfo{ownerID: {ID: ownerID, Username: "alice"}}, nil)
	logic.EXPECT().Delete(mock.Anything, volumeID).Return(nil)
	conn := newCoreGRPCConn(t, func(s *grpc.Server) {
		coregrpc.RegisterVolumeAPI(s, logic, users)
	})
	client := coreapi.NewVolumeAPIClient(conn)

	createResp, err := client.CreateVolume(context.Background(), &coreapi.CreateVolumeRequest{Name: "data"})
	require.NoError(t, err)
	require.Equal(t, volumeID.String(), createResp.VolumeId)
	require.Equal(t, "data", createParams.Name)

	listResp, err := client.ListVolumes(context.Background(), &coreapi.PaginationRequest{Limit: 10})
	require.NoError(t, err)
	require.Len(t, listResp.Volumes, 1)
	require.Equal(t, "alice", listResp.Volumes[0].OwnerUsername)
	require.EqualValues(t, 128, listResp.Volumes[0].UsedBytes)

	_, err = client.DeleteVolume(context.Background(), &coreapi.VolumeActionRequest{VolumeId: volumeID.String()})
	require.NoError(t, err)
}

func TestSystemGRPCHandlerMapsConfigAndErrors(t *testing.T) {
	initial := coretest.SystemConfig()
	logic := coregrpcmocks.NewMockSystemLogic(t)
	var updated config.SystemConfig
	logic.EXPECT().GetConfig(mock.Anything).Return(initial)
	logic.EXPECT().
		UpdateConfig(mock.Anything, mock.AnythingOfType("config.SystemConfig")).
		Run(func(ctx context.Context, cfg config.SystemConfig) {
			updated = cfg
		}).
		Return(nil).
		Once()
	logic.EXPECT().UpdateConfig(mock.Anything, mock.AnythingOfType("config.SystemConfig")).Return(apperrors.ErrBadRequest).Once()
	conn := newCoreGRPCConn(t, func(s *grpc.Server) {
		coregrpc.RegisterSystemAPI(s, logic)
	})
	client := coreapi.NewSystemAPIClient(conn)

	got, err := client.GetConfig(context.Background(), &coreapi.Empty{})
	require.NoError(t, err)
	require.Equal(t, initial.BaseDomain, got.BaseDomain)
	require.Equal(t, initial.BuildPidsLimit, got.BuildPidsLimit)
	require.Equal(t, int32(initial.MaxContainersPerUser), got.MaxContainersPerUser)

	_, err = client.UpdateConfig(context.Background(), &coreapi.SystemConfigData{
		BaseDomain:                    "example.test",
		DefaultMemoryReservationBytes: initial.DefaultMemoryReservation,
		BuildPidsLimit:                initial.BuildPidsLimit,
		MaxContainersPerUser:          int32(initial.MaxContainersPerUser),
	})
	require.NoError(t, err)
	require.Equal(t, "example.test", updated.BaseDomain)

	_, err = client.UpdateConfig(context.Background(), &coreapi.SystemConfigData{})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestImageAndBuildGRPCHandlerMapsResources(t *testing.T) {
	ownerID := uuid.New()
	imageID := uuid.New()
	buildID := uuid.New()
	finishedAt := time.Unix(200, 0)
	imageLogic := coregrpcmocks.NewMockImageLogic(t)
	buildLogic := coregrpcmocks.NewMockBuildLogic(t)
	users := coregrpcmocks.NewMockUserDirectory(t)
	build := model.Build{
		ID:          buildID,
		ImageID:     imageID,
		OwnerID:     ownerID,
		Status:      model.BuildStatusRunning,
		LogFilePath: "build-logs/demo.log",
		StartedAt:   time.Unix(150, 0),
		FinishedAt:  &finishedAt,
	}
	imageLogic.EXPECT().List(mock.Anything, 10, 0).Return([]model.Image{{
		ID:        imageID,
		OwnerID:   ownerID,
		Tag:       "demo:latest",
		SizeMB:    12,
		Status:    model.ImageStatusAvailable,
		CreatedAt: time.Unix(100, 0),
	}}, 1, nil)
	users.EXPECT().GetUsers(mock.Anything, []uuid.UUID{ownerID}).Return(map[uuid.UUID]model.UserInfo{ownerID: {ID: ownerID, Username: "alice"}}, nil).Maybe()
	buildLogic.EXPECT().StartBuildRecord(mock.Anything, buildID).Return(build, true, nil)
	buildLogic.EXPECT().GetBuild(mock.Anything, buildID).Return(build, nil)
	var completedStatus string
	var completedSize int
	buildLogic.EXPECT().
		CompleteBuildRecord(mock.Anything, buildID, imageID, model.BuildStatusSuccess, 20).
		Run(func(ctx context.Context, gotBuildID, gotImageID uuid.UUID, status string, sizeMB int) {
			completedStatus = status
			completedSize = sizeMB
		}).
		Return(nil)
	imageLogic.EXPECT().Delete(mock.Anything, imageID).Return(nil)
	conn := newCoreGRPCConn(t, func(s *grpc.Server) {
		coregrpc.RegisterImageAPI(s, imageLogic, buildLogic, users)
	})
	client := coreapi.NewImageAPIClient(conn)

	images, err := client.ListImages(context.Background(), &coreapi.PaginationRequest{Limit: 10, Page: 1})
	require.NoError(t, err)
	require.Len(t, images.Images, 1)
	require.Equal(t, "alice", images.Images[0].OwnerUsername)
	require.Equal(t, int32(12), images.Images[0].SizeMb)

	started, err := client.StartBuildRecord(context.Background(), &coreapi.BuildActionRequest{BuildId: buildID.String()})
	require.NoError(t, err)
	require.True(t, started.Started)
	require.Equal(t, imageID.String(), started.ImageId)

	buildResp, err := client.GetBuild(context.Background(), &coreapi.BuildActionRequest{BuildId: buildID.String()})
	require.NoError(t, err)
	require.Equal(t, "build-logs/demo.log", buildResp.LogFilePath)
	require.Equal(t, finishedAt.Unix(), buildResp.FinishedAt)

	_, err = client.CompleteBuildRecord(context.Background(), &coreapi.CompleteBuildRequest{BuildId: buildID.String(), ImageId: imageID.String(), Status: model.BuildStatusSuccess, SizeMb: 20})
	require.NoError(t, err)
	require.Equal(t, model.BuildStatusSuccess, completedStatus)
	require.Equal(t, 20, completedSize)

	_, err = client.DeleteImage(context.Background(), &coreapi.ImageActionRequest{ImageId: imageID.String()})
	require.NoError(t, err)

	_, err = client.GetBuild(context.Background(), &coreapi.BuildActionRequest{BuildId: "bad"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestProjectAndStatsGRPCHandlersMapResponses(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	errMsg := "failed"
	projectLogic := coregrpcmocks.NewMockProjectLogic(t)
	statsLogic := coregrpcmocks.NewMockStatsLogic(t)
	users := coregrpcmocks.NewMockUserDirectory(t)
	projectLogic.EXPECT().List(mock.Anything, 20, 20).Return([]model.Project{{
		ID:           projectID,
		OwnerID:      ownerID,
		Name:         "demo",
		Status:       model.ProjectStatusFailed,
		ErrorMessage: &errMsg,
		CreatedAt:    time.Unix(100, 0),
	}}, 1, nil)
	users.EXPECT().GetUsers(mock.Anything, []uuid.UUID{ownerID}).Return(map[uuid.UUID]model.UserInfo{ownerID: {ID: ownerID, Username: "alice"}}, nil)
	projectLogic.EXPECT().Cancel(mock.Anything, projectID).Return(nil)
	statsLogic.EXPECT().GetUserStats(mock.Anything).Return(model.UserStats{ContainersTotal: 2, ContainersRunning: 1, ContainersQuota: 10, RamUsedBytes: 128, RamQuotaBytes: 256, DiskUsedMB: 3, DiskQuotaMB: 4, VolumesTotal: 5, VolumesQuota: 6, ImagesTotal: 7, ProjectsTotal: 8}, nil)
	statsLogic.EXPECT().GetSystemMonitoring(mock.Anything).Return(model.SystemMonitoring{CPUPercent: 12.5, MemoryTotalBytes: 100, MemoryUsedBytes: 50, MemoryAvailableBytes: 50, DiskTotalBytes: 200, DiskUsedBytes: 125, DiskFreeBytes: 75, DCMReservedMemoryBytes: 10, DCMDiskUsedBytes: 20, ContainersTotal: 2, ContainersRunning: 1, ObservedAt: time.Unix(999, 0)}, nil)
	conn := newCoreGRPCConn(t, func(s *grpc.Server) {
		coregrpc.RegisterProjectAPI(s, projectLogic, users)
		coregrpc.RegisterStatsAPI(s, statsLogic)
	})
	projectClient := coreapi.NewProjectAPIClient(conn)
	statsClient := coreapi.NewStatsAPIClient(conn)

	projects, err := projectClient.ListProjects(context.Background(), &coreapi.PaginationRequest{Limit: 20, Page: 2})
	require.NoError(t, err)
	require.Len(t, projects.Projects, 1)
	require.Equal(t, "alice", projects.Projects[0].OwnerUsername)
	require.Equal(t, errMsg, projects.Projects[0].ErrorMessage)

	_, err = projectClient.CancelProject(context.Background(), &coreapi.ProjectActionRequest{ProjectId: projectID.String()})
	require.NoError(t, err)

	userStats, err := statsClient.GetUserStats(context.Background(), &coreapi.Empty{})
	require.NoError(t, err)
	require.Equal(t, int32(2), userStats.ContainersTotal)
	require.EqualValues(t, 128, userStats.RamUsedBytes)

	systemStats, err := statsClient.GetSystemMonitoring(context.Background(), &coreapi.Empty{})
	require.NoError(t, err)
	require.Equal(t, 12.5, systemStats.CpuPercent)
	require.Equal(t, int64(999), systemStats.ObservedAt)
}

func TestReportGRPCHandlerMapsReportsAndAudit(t *testing.T) {
	ownerID := uuid.New()
	actorID := uuid.New()
	eventID := uuid.New()
	from := time.Unix(1000, 0).UTC()
	to := time.Unix(2000, 0).UTC()
	logic := coregrpcmocks.NewMockReportLogic(t)
	var overviewFrom time.Time
	var overviewTo time.Time
	var usageSort string
	var usageSearch string
	var usageLimit int
	var usageOffset int
	var timelineOwnerID uuid.UUID
	var auditFilters model.AuditEventFilters
	var recorded model.AuditEvent
	lastSnapshotAt := from.Add(5 * time.Minute)
	logic.EXPECT().
		GetReportsOverview(mock.Anything, from, to).
		Run(func(ctx context.Context, gotFrom, gotTo time.Time) {
			overviewFrom = gotFrom
			overviewTo = gotTo
		}).
		Return(model.ReportsOverview{
			From:                         from,
			To:                           to,
			AuditEventsTotal:             10,
			FailedActionsTotal:           2,
			ActiveUsersTotal:             3,
			MemoryUsageBytes:             900,
			ReservedMemoryBytes:          1024,
			CPUPercent:                   12.5,
			TotalDiskBytes:               2048,
			ResourcesTotal:               20,
			ContainersTotal:              2,
			ContainersRunning:            1,
			VolumesTotal:                 3,
			ImagesTotal:                  4,
			BuildsTotal:                  5,
			ProjectsTotal:                6,
			LastUsageSnapshotAt:          &lastSnapshotAt,
			UsageSnapshotIntervalSeconds: 300,
			TopActions:                   []model.ActionCount{{Action: "container.create", Count: 5}},
		}, nil)
	logic.EXPECT().
		ListUserUsageReport(mock.Anything, from, to, "disk_desc", "ali", 10, 10).
		Run(func(ctx context.Context, gotFrom, gotTo time.Time, sort, search string, limit, offset int) {
			usageSort = sort
			usageSearch = search
			usageLimit = limit
			usageOffset = offset
		}).
		Return([]model.UserUsageReportItem{{
			OwnerID:             ownerID,
			OwnerUsername:       "alice",
			MemoryUsageBytes:    384,
			ReservedMemoryBytes: 512,
			CPUPercent:          7.5,
			TotalDiskBytes:      1024,
			ResourcesTotal:      20,
			ContainersTotal:     2,
			ContainersRunning:   1,
			VolumesTotal:        3,
			ImagesTotal:         4,
			BuildsTotal:         5,
			ProjectsTotal:       6,
			ActionsTotal:        7,
		}}, 1, nil)
	logic.EXPECT().
		GetUserUsageTimeline(mock.Anything, ownerID, from, to).
		Run(func(ctx context.Context, gotOwnerID uuid.UUID, gotFrom, gotTo time.Time) {
			timelineOwnerID = gotOwnerID
		}).
		Return([]model.UserUsagePoint{{
			BucketStart:         from,
			MemoryUsageBytes:    64,
			ReservedMemoryBytes: 128,
			CPUPercent:          2.5,
			TotalDiskBytes:      256,
			ResourcesTotal:      14,
			ContainersTotal:     2,
			ContainersRunning:   1,
			VolumesTotal:        3,
			ImagesTotal:         4,
			BuildsTotal:         5,
			ProjectsTotal:       6,
			ActionsTotal:        4,
		}}, nil)
	logic.EXPECT().
		ListAuditEvents(mock.Anything, mock.AnythingOfType("model.AuditEventFilters")).
		Run(func(ctx context.Context, filters model.AuditEventFilters) {
			auditFilters = filters
		}).
		Return([]model.AuditEvent{{
			ID:            eventID,
			OccurredAt:    from,
			ActorUserID:   &actorID,
			ActorUsername: "admin",
			ActorScope:    "admin",
			Action:        "container.delete",
			Outcome:       "success",
			ResourceType:  "container",
			ResourceID:    "docker-id",
			ResourceName:  "web",
			OwnerID:       &ownerID,
			OwnerUsername: "alice",
			RequestID:     "req-1",
			ClientIP:      "127.0.0.1",
			UserAgent:     "tests",
			DetailsJSON:   `{"safe":true}`,
		}}, 1, nil)
	logic.EXPECT().
		RecordAuditEvent(mock.Anything, mock.AnythingOfType("model.AuditEvent")).
		Run(func(ctx context.Context, event model.AuditEvent) {
			recorded = event
		}).
		Return(nil)
	logic.EXPECT().
		RefreshUsageSnapshots(mock.Anything).
		Return(model.UsageSnapshotCollection{BucketStart: from, CollectedAt: to, SnapshotsCount: 2}, nil)
	conn := newCoreGRPCConn(t, func(s *grpc.Server) {
		coregrpc.RegisterReportAPI(s, logic)
	})
	client := coreapi.NewReportAPIClient(conn)

	overview, err := client.GetReportsOverview(context.Background(), &coreapi.ReportRangeRequest{From: from.Unix(), To: to.Unix()})
	require.NoError(t, err)
	require.Equal(t, int64(10), overview.AuditEventsTotal)
	require.Equal(t, int64(900), overview.MemoryUsageBytes)
	require.Equal(t, int64(1024), overview.ReservedMemoryBytes)
	require.Equal(t, 12.5, overview.CpuPercent)
	require.Equal(t, int64(20), overview.ResourcesTotal)
	require.Equal(t, lastSnapshotAt.Unix(), overview.LastUsageSnapshotAt)
	require.Equal(t, int64(300), overview.UsageSnapshotIntervalSeconds)
	require.Len(t, overview.TopActions, 1)
	require.Equal(t, "container.create", overview.TopActions[0].Action)
	require.Equal(t, from, overviewFrom)
	require.Equal(t, to, overviewTo)

	usage, err := client.ListUserUsageReport(context.Background(), &coreapi.ListUserUsageReportRequest{
		From: from.Unix(), To: to.Unix(), Sort: "disk_desc", Search: "ali", Page: 2, Limit: 10,
	})
	require.NoError(t, err)
	require.Equal(t, int32(1), usage.TotalCount)
	require.Equal(t, ownerID.String(), usage.Users[0].OwnerId)
	require.Equal(t, int64(384), usage.Users[0].MemoryUsageBytes)
	require.Equal(t, 7.5, usage.Users[0].CpuPercent)
	require.Equal(t, int32(20), usage.Users[0].ResourcesTotal)
	require.Equal(t, "disk_desc", usageSort)
	require.Equal(t, "ali", usageSearch)
	require.Equal(t, 10, usageLimit)
	require.Equal(t, 10, usageOffset)

	timeline, err := client.GetUserUsageTimeline(context.Background(), &coreapi.GetUserUsageTimelineRequest{
		OwnerId: ownerID.String(), From: from.Unix(), To: to.Unix(),
	})
	require.NoError(t, err)
	require.Len(t, timeline.Points, 1)
	require.Equal(t, int64(64), timeline.Points[0].MemoryUsageBytes)
	require.Equal(t, int64(128), timeline.Points[0].ReservedMemoryBytes)
	require.Equal(t, 2.5, timeline.Points[0].CpuPercent)
	require.Equal(t, int32(14), timeline.Points[0].ResourcesTotal)
	require.Equal(t, ownerID, timelineOwnerID)

	events, err := client.ListAuditEvents(context.Background(), &coreapi.ListAuditEventsRequest{
		From: from.Unix(), To: to.Unix(), ActorUserId: actorID.String(), Action: "container.delete",
		Outcome: "success", ResourceType: "container", Search: "web", Page: 3, Limit: 5,
	})
	require.NoError(t, err)
	require.Equal(t, int32(1), events.TotalCount)
	require.Equal(t, eventID.String(), events.Events[0].Id)
	require.Equal(t, ownerID.String(), events.Events[0].OwnerId)
	require.Equal(t, actorID, *auditFilters.ActorUserID)
	require.Equal(t, "container.delete", auditFilters.Action)
	require.Equal(t, 5, auditFilters.Limit)
	require.Equal(t, 10, auditFilters.Offset)

	_, err = client.RecordAuditEvent(context.Background(), &coreapi.AuditEventData{
		Id: eventID.String(), OccurredAt: to.Unix(), ActorUserId: actorID.String(), OwnerId: ownerID.String(),
		ActorUsername: "admin", Action: "image.push", Outcome: "failed", ErrorCode: "denied",
	})
	require.NoError(t, err)
	require.Equal(t, eventID, recorded.ID)
	require.Equal(t, actorID, *recorded.ActorUserID)
	require.Equal(t, ownerID, *recorded.OwnerID)
	require.Equal(t, "denied", recorded.ErrorCode)

	refresh, err := client.RefreshUsageSnapshots(context.Background(), &coreapi.Empty{})
	require.NoError(t, err)
	require.Equal(t, from.Unix(), refresh.BucketStart)
	require.Equal(t, to.Unix(), refresh.CollectedAt)
	require.Equal(t, int32(2), refresh.SnapshotsCount)

	_, err = client.GetUserUsageTimeline(context.Background(), &coreapi.GetUserUsageTimelineRequest{OwnerId: "bad"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = client.ListAuditEvents(context.Background(), &coreapi.ListAuditEventsRequest{ActorUserId: "bad"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func newCoreGRPCConn(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
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
