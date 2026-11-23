package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/fieldfill"
	gatewayhandlermocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/gateway/http/handler"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// A rejected request tells the client which limit it hit, with the client
// code and status that kind of rejection always had.
func TestCoreHandlerNamesTheExceededLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		limit  apperrors.Limit
		status int
		code   string
	}{
		{apperrors.LimitUserCPU, http.StatusConflict, "quota_exceeded"},
		{apperrors.LimitContainersPerUser, http.StatusConflict, "limit_exceeded"},
		{apperrors.LimitHostMemory, http.StatusServiceUnavailable, "host_exhausted"},
	} {
		t.Run(string(tc.limit), func(t *testing.T) {
			svc := newCoreServiceMock(t)
			svc.EXPECT().CreateContainer(mock.Anything, mock.Anything).Return("", apperrors.Exceeded(tc.limit))

			resp := perform(coreRouter(svc), http.MethodPost, "/containers", `{"name":"web","image_tag":"nginx:latest"}`, nil)

			require.Equal(t, tc.status, resp.Code)
			var body map[string]any
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
			require.Equal(t, tc.code, body["error_code"])
			require.Equal(t, string(tc.limit), body["limit"])
		})
	}
}

func TestCoreHandlerContainerVolumeAndStatsEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newCoreServiceMock(t)
	router := coreRouter(svc)
	containerID := uuid.NewString()
	volumeID := uuid.NewString()
	var createInput model.CreateContainerInput

	svc.EXPECT().
		CreateContainer(mock.Anything, mock.AnythingOfType("model.CreateContainerInput")).
		Run(func(ctx context.Context, input model.CreateContainerInput) {
			createInput = input
		}).
		Return(containerID, nil)
	resp := perform(router, http.MethodPost, "/containers", `{"name":"web","image_tag":"nginx:latest","internal_port":80,"domain_prefix":"web","env_vars":{"APP_ENV":"test"},"volume_mounts":[{"volume_id":"`+volumeID+`","mount_path":"/data","is_readonly":true}],"memory_reservation_mb":512,"cpu_reservation_millicores":750}`, nil)
	require.Equal(t, http.StatusAccepted, resp.Code)
	require.Contains(t, resp.Body.String(), containerID)
	require.Equal(t, "web", createInput.Name)
	require.EqualValues(t, 512, createInput.MemoryReservationMB)
	require.EqualValues(t, 750, createInput.CPUReservationMillicores)
	require.Len(t, createInput.VolumeMounts, 1)
	require.True(t, createInput.VolumeMounts[0].IsReadOnly)

	projectID := uuid.NewString()
	svc.EXPECT().ListContainers(mock.Anything, 2, 10, projectID).Return(model.PaginatedContainers{
		Containers: []model.Container{{ID: containerID, DockerID: "docker-id", ProjectID: projectID, Name: "web", OwnerID: "owner-id", OwnerUsername: "alice"}},
		TotalCount: 1,
	}, nil)
	resp = perform(router, http.MethodGet, "/containers?page=2&limit=10&project_id="+projectID, ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"project_id":"`+projectID+`"`)
	require.NotContains(t, resp.Body.String(), "docker_id")
	require.NotContains(t, resp.Body.String(), "owner_id")
	svc.EXPECT().ListContainers(mock.Anything, 2, 10, "").Return(model.PaginatedContainers{
		Containers: []model.Container{{ID: containerID, DockerID: "docker-id", Name: "web", OwnerID: "owner-id", OwnerUsername: "alice"}},
		TotalCount: 1,
	}, nil)
	resp = perform(router, http.MethodGet, "/admin/containers?page=2&limit=10", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), "docker_id")
	require.Contains(t, resp.Body.String(), "owner_id")

	svc.EXPECT().GetContainer(mock.Anything, containerID).Return(model.Container{ID: containerID, DockerID: "docker-id", Name: "web", Status: "running", OwnerID: "owner-id", OwnerUsername: "alice", MemoryReservationBytes: 512 << 20, CPUReservationMillicores: 750}, nil).Twice()
	resp = perform(router, http.MethodGet, "/containers/"+containerID, ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"memory_reservation_bytes":536870912`)
	require.Contains(t, resp.Body.String(), `"cpu_reservation_millicores":750`)
	require.Contains(t, resp.Body.String(), `"id":"`+containerID+`"`)
	require.Contains(t, resp.Body.String(), `"status":"running"`)
	require.NotContains(t, resp.Body.String(), "docker_id")
	require.NotContains(t, resp.Body.String(), "owner_id")
	require.NotContains(t, resp.Body.String(), "owner_username")
	resp = perform(router, http.MethodGet, "/admin/containers/"+containerID, ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"docker_id":"docker-id"`)
	require.Contains(t, resp.Body.String(), `"owner_username":"alice"`)
	require.Contains(t, resp.Body.String(), `"memory_reservation_bytes":536870912`)
	svc.EXPECT().GetContainer(mock.Anything, containerID).Return(model.Container{}, apperrors.ErrNotFound)
	resp = perform(router, http.MethodGet, "/containers/"+containerID, ``, nil)
	require.Equal(t, http.StatusNotFound, resp.Code)

	svc.EXPECT().ActionContainer(mock.Anything, containerID, "start").Return(nil)
	resp = perform(router, http.MethodPost, "/containers/"+containerID+"/action/start", ``, nil)
	require.Equal(t, http.StatusAccepted, resp.Code)
	require.Contains(t, resp.Body.String(), "start requested")

	svc.EXPECT().ExposeContainer(mock.Anything, containerID, "web", 8080).Return(nil)
	resp = perform(router, http.MethodPost, "/containers/"+containerID+"/expose", `{"domain_prefix":"web","internal_port":8080}`, nil)
	require.Equal(t, http.StatusAccepted, resp.Code)

	svc.EXPECT().GetContainerStats(mock.Anything, containerID).Return(model.ContainerStats{CPUPercentage: 12.5, MemoryLimitBytes: 3072, MemoryStandardLimitBytes: 2048}, nil)
	resp = perform(router, http.MethodGet, "/containers/"+containerID+"/stats", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"cpu_percentage":12.5`)
	require.Contains(t, resp.Body.String(), `"memory_limit_bytes":3072`)
	require.Contains(t, resp.Body.String(), `"memory_standard_limit_bytes":2048`)

	svc.EXPECT().CreateVolume(mock.Anything, model.CreateVolumeInput{Name: "data"}).Return(volumeID, nil)
	resp = perform(router, http.MethodPost, "/volumes", `{"name":"data"}`, nil)
	require.Equal(t, http.StatusAccepted, resp.Code)
	require.Contains(t, resp.Body.String(), volumeID)
	svc.EXPECT().CreateVolume(mock.Anything, model.CreateVolumeInput{Name: "sized", SizeBytes: 33554432}).Return(volumeID, nil)
	resp = perform(router, http.MethodPost, "/volumes", `{"name":"sized","size_bytes":33554432}`, nil)
	require.Equal(t, http.StatusAccepted, resp.Code)

	svc.EXPECT().ListVolumes(mock.Anything, 1, 20).Return(model.PaginatedVolumes{Volumes: []model.Volume{{ID: volumeID, Name: "data", DockerName: "vol_data", OwnerID: "owner-id", SizeBytes: 1073741824, SizeEnforced: true, UsedBy: []model.VolumeUser{{ContainerID: "container-id", Name: "web"}}}, {ID: "free-id", Name: "free"}}, TotalCount: 2}, nil).Twice()
	resp = perform(router, http.MethodGet, "/volumes", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"name":"data"`)
	require.Contains(t, resp.Body.String(), `"size_bytes":1073741824`)
	require.Contains(t, resp.Body.String(), `"size_enforced":true`)
	require.Contains(t, resp.Body.String(), `"used_by":[{"id":"container-id","name":"web"}]`)
	require.Contains(t, resp.Body.String(), `"used_by":[]`, "a volume no container mounts names an empty list")
	require.NotContains(t, resp.Body.String(), "owner_id")
	require.NotContains(t, resp.Body.String(), "docker_name")
	resp = perform(router, http.MethodGet, "/admin/volumes", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), "owner_id")
	require.Contains(t, resp.Body.String(), "docker_name")
	require.Contains(t, resp.Body.String(), `"size_bytes":1073741824`)
	require.Contains(t, resp.Body.String(), `"used_by":[{"id":"container-id","name":"web"}]`)

	svc.EXPECT().DeleteVolume(mock.Anything, volumeID).Return(nil)
	resp = perform(router, http.MethodDelete, "/volumes/"+volumeID, ``, nil)
	require.Equal(t, http.StatusAccepted, resp.Code)
}

func TestCoreHandlerImagesBuildsProjectsSystemStatsAndReports(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newCoreServiceMock(t)
	router := coreRouter(svc)
	imageID := uuid.NewString()
	buildID := uuid.NewString()
	projectID := uuid.NewString()
	ownerID := uuid.NewString()

	svc.EXPECT().ListImages(mock.Anything, 1, 20).Return(model.PaginatedImages{Images: []model.Image{{ID: imageID, OwnerID: ownerID, OwnerUsername: "alice", Tag: "demo:latest", SizeBytes: 2<<20 + 123, UsedBy: []model.ImageUser{{ContainerID: "container-id", Name: "web"}}}, {ID: uuid.NewString(), Tag: "free:latest"}}, TotalCount: 2}, nil)
	resp := perform(router, http.MethodGet, "/images", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.NotContains(t, resp.Body.String(), "owner_id")
	require.Contains(t, resp.Body.String(), `"size_bytes":2097275`, "an image reports its exact size")
	require.NotContains(t, resp.Body.String(), "size_mb")
	require.Contains(t, resp.Body.String(), `"used_by":[{"id":"container-id","name":"web"}]`)
	require.Contains(t, resp.Body.String(), `"used_by":[]`, "an image no container uses names an empty list")

	svc.EXPECT().DeleteImage(mock.Anything, imageID).Return(nil)
	resp = perform(router, http.MethodDelete, "/images/"+imageID, ``, nil)
	require.Equal(t, http.StatusAccepted, resp.Code)

	svc.EXPECT().ListBuilds(mock.Anything, 1, 20).Return(model.PaginatedBuilds{Builds: []model.Build{{ID: buildID, Tag: "app:v1", LogObjectKey: "build-logs/demo.log", OwnerID: ownerID}}, TotalCount: 1}, nil)
	resp = perform(router, http.MethodGet, "/builds", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.NotContains(t, resp.Body.String(), "log_object_key")
	// A build whose image record is gone still names its tag, and no image.
	require.Contains(t, resp.Body.String(), `"tag":"app:v1"`)
	require.NotContains(t, resp.Body.String(), "image_id")

	gitBuild := model.Build{ID: buildID, Tag: "app:v1", Status: "running", SourceKind: "git", SourceLocation: "https://github.com/acme/app.git", SourceRef: "main", LogObjectKey: "build-logs/demo.log", OwnerID: ownerID, OwnerUsername: "alice"}
	svc.EXPECT().GetBuild(mock.Anything, buildID).Return(gitBuild, nil).Twice()
	resp = perform(router, http.MethodGet, "/builds/"+buildID, ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"id":"`+buildID+`"`)
	require.Contains(t, resp.Body.String(), `"source_kind":"git"`)
	require.Contains(t, resp.Body.String(), `"source_location":"https://github.com/acme/app.git"`)
	require.Contains(t, resp.Body.String(), `"source_ref":"main"`)
	require.NotContains(t, resp.Body.String(), "log_object_key")
	require.NotContains(t, resp.Body.String(), "owner_username")
	resp = perform(router, http.MethodGet, "/admin/builds/"+buildID, ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"owner_username":"alice"`)
	require.Contains(t, resp.Body.String(), `"source_kind":"git"`)
	svc.EXPECT().GetBuild(mock.Anything, buildID).Return(model.Build{}, apperrors.ErrNotFound).Once()
	resp = perform(router, http.MethodGet, "/builds/"+buildID, ``, nil)
	require.Equal(t, http.StatusNotFound, resp.Code)
	resp = perform(router, http.MethodGet, "/builds/not-a-uuid", ``, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)

	svc.EXPECT().CancelBuildRecord(mock.Anything, buildID).Return(nil)
	resp = perform(router, http.MethodPost, "/builds/"+buildID+"/cancel", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	svc.EXPECT().DeleteBuild(mock.Anything, buildID).Return(nil)
	resp = perform(router, http.MethodDelete, "/builds/"+buildID, ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)

	svc.EXPECT().ListProjects(mock.Anything, 1, 20).Return(model.PaginatedProjects{Projects: []model.Project{{ID: projectID, OwnerID: ownerID, OwnerUsername: "alice"}}, TotalCount: 1}, nil)
	resp = perform(router, http.MethodGet, "/projects", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.NotContains(t, resp.Body.String(), "owner_id")
	svc.EXPECT().GetProject(mock.Anything, projectID).Return(model.Project{ID: projectID, Name: "shop", Status: "running", OwnerID: ownerID, OwnerUsername: "alice"}, nil).Twice()
	resp = perform(router, http.MethodGet, "/projects/"+projectID, ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"id":"`+projectID+`"`)
	require.Contains(t, resp.Body.String(), `"name":"shop"`)
	require.NotContains(t, resp.Body.String(), "owner_id")
	require.NotContains(t, resp.Body.String(), "owner_username")
	resp = perform(router, http.MethodGet, "/admin/projects/"+projectID, ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"owner_username":"alice"`)
	svc.EXPECT().GetProject(mock.Anything, projectID).Return(model.Project{}, apperrors.ErrNotFound)
	resp = perform(router, http.MethodGet, "/projects/"+projectID, ``, nil)
	require.Equal(t, http.StatusNotFound, resp.Code)
	resp = perform(router, http.MethodGet, "/projects/not-a-uuid", ``, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	svc.EXPECT().StartProject(mock.Anything, projectID).Return(nil)
	resp = perform(router, http.MethodPost, "/projects/"+projectID+"/start", ``, nil)
	require.Equal(t, http.StatusAccepted, resp.Code)
	svc.EXPECT().StopProject(mock.Anything, projectID).Return(nil)
	resp = perform(router, http.MethodPost, "/projects/"+projectID+"/stop", ``, nil)
	require.Equal(t, http.StatusAccepted, resp.Code)
	svc.EXPECT().CancelProject(mock.Anything, projectID).Return(nil)
	resp = perform(router, http.MethodPost, "/projects/"+projectID+"/cancel", ``, nil)
	require.Equal(t, http.StatusAccepted, resp.Code)
	svc.EXPECT().DeleteProject(mock.Anything, projectID).Return(nil)
	resp = perform(router, http.MethodDelete, "/projects/"+projectID, ``, nil)
	require.Equal(t, http.StatusAccepted, resp.Code)

	svc.EXPECT().GetSystemConfig(mock.Anything).Return(model.SystemConfig{MaxLogSize: "25m", DefaultVolumeSizeBytes: 1073741824, VolumeSizeEnforcement: true, DefaultUserQuotaCPU: 1.5, DefaultUserQuotaRAMMB: 3072, DefaultUserQuotaDiskMB: 6144, BuildMaxDiskBytes: 3221225472}, nil)
	resp = perform(router, http.MethodGet, "/admin/config", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"max_log_size":"25m"`)
	require.Contains(t, resp.Body.String(), `"default_volume_size_bytes":1073741824`)
	require.Contains(t, resp.Body.String(), `"volume_size_enforcement":true`)
	require.Contains(t, resp.Body.String(), `"default_user_quota_cpu":1.5`)
	require.Contains(t, resp.Body.String(), `"default_user_quota_ram_mb":3072`)
	require.Contains(t, resp.Body.String(), `"default_user_quota_disk_mb":6144`)
	require.Contains(t, resp.Body.String(), `"build_max_disk_bytes":3221225472`)
	var updatedConfig model.SystemConfig
	svc.EXPECT().UpdateSystemConfig(mock.Anything, mock.AnythingOfType("model.SystemConfig")).
		Run(func(_ context.Context, cfg model.SystemConfig) { updatedConfig = cfg }).Return(nil)
	resp = perform(router, http.MethodPut, "/admin/config", `{"base_domain":"ignored.example.test","default_volume_size_bytes":2097152,"volume_size_enforcement":true,"default_user_quota_cpu":2.5,"default_user_quota_ram_mb":4096,"default_user_quota_disk_mb":10240,"build_max_disk_bytes":5368709120}`, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.EqualValues(t, 2097152, updatedConfig.DefaultVolumeSizeBytes)
	require.True(t, updatedConfig.VolumeSizeEnforcement)
	require.Equal(t, 2.5, updatedConfig.DefaultUserQuotaCPU)
	require.Equal(t, int64(4096), updatedConfig.DefaultUserQuotaRAMMB)
	require.Equal(t, int64(10240), updatedConfig.DefaultUserQuotaDiskMB)
	require.Equal(t, int64(5368709120), updatedConfig.BuildMaxDiskBytes)

	svc.EXPECT().GetUserStats(mock.Anything).Return(model.UserStats{ContainersTotal: 2, CPUUsedMillicores: 750, CPUQuotaMillicores: 2000}, nil)
	resp = perform(router, http.MethodGet, "/stats", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"cpu_used_millicores":750`)
	require.Contains(t, resp.Body.String(), `"cpu_quota_millicores":2000`)
	svc.EXPECT().GetSystemMonitoring(mock.Anything).Return(model.SystemMonitoring{ContainersTotal: 3, DCMBorrowedMemoryBytes: 512, DCMReservedCPUMillicores: 750, DCMReservedBuildCPUMillicores: 500, RootfsQuotaStatus: "unknown", DCMReservedVolumeBytes: 4096, DCMUnusedVolumePromiseBytes: 1024, BuildClaimCap: 1, MaxConcurrentBuilds: 2, VolumeSizeMode: "measured"}, nil)
	resp = perform(router, http.MethodGet, "/admin/monitoring", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"dcm_borrowed_memory_bytes":512`)
	require.Contains(t, resp.Body.String(), `"dcm_reserved_cpu_millicores":750`)
	require.Contains(t, resp.Body.String(), `"dcm_reserved_build_cpu_millicores":500`)
	require.Contains(t, resp.Body.String(), `"rootfs_quota_status":"unknown"`)
	require.Contains(t, resp.Body.String(), `"dcm_reserved_volume_bytes":4096`)
	require.Contains(t, resp.Body.String(), `"dcm_unused_volume_promise_bytes":1024`)
	require.Contains(t, resp.Body.String(), `"build_claim_cap":1`)
	require.Contains(t, resp.Body.String(), `"max_concurrent_builds":2`)
	require.Contains(t, resp.Body.String(), `"volume_size_mode":"measured"`)

	svc.EXPECT().GetReportsOverview(mock.Anything, int64(1), int64(2)).Return(model.ReportsOverview{From: 1, To: 2}, nil)
	resp = perform(router, http.MethodGet, "/admin/reports/overview?from=1&to=2", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	svc.EXPECT().ListUserUsageReport(mock.Anything, int64(1), int64(2), "disk", "alice", 1, 20).Return([]model.UserUsageReportItem{{OwnerID: ownerID, OwnerUsername: "alice"}}, 1, nil)
	resp = perform(router, http.MethodGet, "/admin/reports/users?from=1&to=2&search=alice", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	svc.EXPECT().GetUserUsageTimeline(mock.Anything, ownerID, int64(1), int64(2)).Return([]model.UserUsagePoint{{BucketStart: 1}}, nil)
	resp = perform(router, http.MethodGet, "/admin/reports/users/"+ownerID+"/usage?from=1&to=2", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	svc.EXPECT().ListAuditEvents(mock.Anything, mock.MatchedBy(func(filters model.AuditEventFilters) bool {
		return filters.From == 1 && filters.To == 2 && filters.ActorUserID == ownerID
	})).Return([]model.AuditEvent{{ActorUserID: ownerID, Action: "containers.create"}}, 1, nil)
	resp = perform(router, http.MethodGet, "/admin/reports/audit-events?from=1&to=2&actor_user_id="+ownerID, ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	svc.EXPECT().RefreshUsageSnapshots(mock.Anything).Return(model.RefreshUsageSnapshotsResult{SnapshotsCount: 1}, nil)
	resp = perform(router, http.MethodPost, "/admin/reports/usage-snapshots/refresh", ``, nil)
	require.Equal(t, http.StatusAccepted, resp.Code)
}

func TestCoreHandlerRejectsInvalidInputAndMapsErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newCoreServiceMock(t)
	router := coreRouter(svc)
	id := uuid.NewString()

	resp := perform(router, http.MethodGet, "/containers?page=0", ``, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	resp = perform(router, http.MethodGet, "/containers?project_id=not-a-uuid", ``, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	resp = perform(router, http.MethodGet, "/containers/not-a-uuid", ``, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	resp = perform(router, http.MethodPost, "/containers/not-a-uuid/action/start", ``, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	resp = perform(router, http.MethodPost, "/containers/"+id+"/action/restart", ``, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	resp = perform(router, http.MethodPost, "/containers", `{"name":"bad name","image_tag":"nginx:latest","internal_port":80}`, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)

	svc.EXPECT().DeleteImage(mock.Anything, id).Return(apperrors.ErrNotFound)
	resp = perform(router, http.MethodDelete, "/images/"+id, ``, nil)
	require.Equal(t, http.StatusNotFound, resp.Code)
}

func TestCoreHandlerLogsFailedAuditWriteWithoutChangingResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := gatewayhandlermocks.NewMockCoreService(t)
	containerID := uuid.NewString()
	svc.EXPECT().ActionContainer(mock.Anything, containerID, "start").Return(nil)
	svc.EXPECT().RecordAuditEvent(mock.Anything, mock.AnythingOfType("model.AuditEvent")).Return(errors.New("core unavailable"))
	var logs bytes.Buffer
	h := handler.NewCoreHandler(svc)
	router := gin.New()
	router.Use(logging.AccessLogMiddleware(slog.New(slog.NewJSONHandler(&logs, nil))))
	router.POST("/containers/:id/action/:action", h.ActionContainer)

	resp := perform(router, http.MethodPost, "/containers/"+containerID+"/action/start", ``, nil)
	require.Equal(t, http.StatusAccepted, resp.Code)
	require.Contains(t, logs.String(), `"error":"record audit event: core unavailable"`)
}

func newCoreServiceMock(t *testing.T) *gatewayhandlermocks.MockCoreService {
	t.Helper()
	svc := gatewayhandlermocks.NewMockCoreService(t)
	svc.EXPECT().RecordAuditEvent(mock.Anything, mock.AnythingOfType("model.AuditEvent")).Return(nil).Maybe()
	return svc
}

func coreRouter(svc *gatewayhandlermocks.MockCoreService) *gin.Engine {
	h := handler.NewCoreHandler(svc)
	router := gin.New()
	router.POST("/containers", h.CreateContainer)
	router.GET("/containers", h.GetContainers)
	router.GET("/admin/containers", h.ListAdminContainers)
	router.GET("/containers/:id", h.GetContainer)
	router.GET("/admin/containers/:id", h.AdminGetContainer)
	router.POST("/containers/:id/action/:action", h.ActionContainer)
	router.POST("/containers/:id/expose", h.ExposeContainer)
	router.GET("/containers/:id/stats", h.GetContainerStats)
	router.POST("/volumes", h.CreateVolume)
	router.GET("/volumes", h.GetVolumes)
	router.GET("/admin/volumes", h.ListAdminVolumes)
	router.DELETE("/volumes/:id", h.DeleteVolume)
	router.GET("/images", h.GetImages)
	router.DELETE("/images/:id", h.DeleteImage)
	router.GET("/builds", h.GetBuilds)
	router.GET("/builds/:id", h.GetBuild)
	router.GET("/admin/builds/:id", h.AdminGetBuild)
	router.POST("/builds/:id/cancel", h.CancelBuild)
	router.DELETE("/builds/:id", h.DeleteBuild)
	router.GET("/projects", h.GetProjects)
	router.GET("/projects/:id", h.GetProject)
	router.GET("/admin/projects/:id", h.AdminGetProject)
	router.POST("/projects/:id/start", h.StartProject)
	router.POST("/projects/:id/stop", h.StopProject)
	router.POST("/projects/:id/cancel", h.CancelProject)
	router.DELETE("/projects/:id", h.DeleteProject)
	router.GET("/admin/config", h.GetSystemConfig)
	router.PUT("/admin/config", h.UpdateSystemConfig)
	router.GET("/stats", h.GetUserStats)
	router.GET("/admin/monitoring", h.GetSystemMonitoring)
	router.GET("/admin/reports/overview", h.GetReportsOverview)
	router.GET("/admin/reports/users", h.ListUserUsageReport)
	router.GET("/admin/reports/users/:id/usage", h.GetUserUsageTimeline)
	router.GET("/admin/reports/audit-events", h.ListAuditEvents)
	router.POST("/admin/reports/usage-snapshots/refresh", h.RefreshUsageSnapshots)
	return router
}

// Every runtime setting is mapped between Gateway's model and the HTTP body in
// both directions: what a read returns can be sent back unchanged.
func TestCoreHandlerCarriesEveryRuntimeSetting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newCoreServiceMock(t)
	router := coreRouter(svc)
	var settings model.SystemConfig
	fieldfill.Distinct(t, &settings)

	svc.EXPECT().GetSystemConfig(mock.Anything).Return(settings, nil)
	resp := perform(router, http.MethodGet, "/admin/config", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	require.Len(t, body, reflect.TypeFor[model.SystemConfig]().NumField(), "a read returns one key per runtime setting")

	var updated model.SystemConfig
	svc.EXPECT().UpdateSystemConfig(mock.Anything, mock.AnythingOfType("model.SystemConfig")).
		Run(func(_ context.Context, cfg model.SystemConfig) { updated = cfg }).Return(nil)
	resp = perform(router, http.MethodPut, "/admin/config", resp.Body.String(), nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Equal(t, settings, updated)
}
