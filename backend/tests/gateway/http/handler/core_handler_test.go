package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	gatewayhandlermocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/gateway/http/handler"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

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
	resp := perform(router, http.MethodPost, "/containers", `{"name":"web","image_tag":"nginx:latest","internal_port":80,"domain_prefix":"web","env_vars":{"APP_ENV":"test"},"volume_mounts":[{"volume_id":"`+volumeID+`","mount_path":"/data","is_readonly":true}]}`, nil)
	require.Equal(t, http.StatusAccepted, resp.Code)
	require.Contains(t, resp.Body.String(), containerID)
	require.Equal(t, "web", createInput.Name)
	require.Len(t, createInput.VolumeMounts, 1)
	require.True(t, createInput.VolumeMounts[0].IsReadOnly)

	svc.EXPECT().ListContainers(mock.Anything, 2, 10).Return(model.PaginatedContainers{
		Containers: []model.Container{{ID: containerID, DockerID: "docker-id", Name: "web", OwnerID: "owner-id", OwnerUsername: "alice"}},
		TotalCount: 1,
	}, nil).Twice()
	resp = perform(router, http.MethodGet, "/containers?page=2&limit=10", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.NotContains(t, resp.Body.String(), "docker_id")
	require.NotContains(t, resp.Body.String(), "owner_id")
	resp = perform(router, http.MethodGet, "/admin/containers?page=2&limit=10", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), "docker_id")
	require.Contains(t, resp.Body.String(), "owner_id")

	svc.EXPECT().ActionContainer(mock.Anything, containerID, "start").Return(nil)
	resp = perform(router, http.MethodPost, "/containers/"+containerID+"/action/start", ``, nil)
	require.Equal(t, http.StatusAccepted, resp.Code)
	require.Contains(t, resp.Body.String(), "start requested")

	svc.EXPECT().ExposeContainer(mock.Anything, containerID, "web", 8080).Return(nil)
	resp = perform(router, http.MethodPost, "/containers/"+containerID+"/expose", `{"domain_prefix":"web","internal_port":8080}`, nil)
	require.Equal(t, http.StatusAccepted, resp.Code)

	svc.EXPECT().GetContainerStats(mock.Anything, containerID).Return(model.ContainerStats{CPUPercentage: 12.5}, nil)
	resp = perform(router, http.MethodGet, "/containers/"+containerID+"/stats", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"cpu_percentage":12.5`)

	svc.EXPECT().CreateVolume(mock.Anything, model.CreateVolumeInput{Name: "data"}).Return(volumeID, nil)
	resp = perform(router, http.MethodPost, "/volumes", `{"name":"data"}`, nil)
	require.Equal(t, http.StatusAccepted, resp.Code)
	require.Contains(t, resp.Body.String(), volumeID)

	svc.EXPECT().ListVolumes(mock.Anything, 1, 20).Return(model.PaginatedVolumes{Volumes: []model.Volume{{ID: volumeID, Name: "data", DockerName: "vol_data", OwnerID: "owner-id"}}, TotalCount: 1}, nil).Twice()
	resp = perform(router, http.MethodGet, "/volumes", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"name":"data"`)
	require.NotContains(t, resp.Body.String(), "owner_id")
	require.NotContains(t, resp.Body.String(), "docker_name")
	resp = perform(router, http.MethodGet, "/admin/volumes", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), "owner_id")
	require.Contains(t, resp.Body.String(), "docker_name")

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

	svc.EXPECT().ListImages(mock.Anything, 1, 20).Return(model.PaginatedImages{Images: []model.Image{{ID: imageID, OwnerID: ownerID, OwnerUsername: "alice", Tag: "demo:latest"}}, TotalCount: 1}, nil)
	resp := perform(router, http.MethodGet, "/images", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.NotContains(t, resp.Body.String(), "owner_id")

	svc.EXPECT().DeleteImage(mock.Anything, imageID).Return(nil)
	resp = perform(router, http.MethodDelete, "/images/"+imageID, ``, nil)
	require.Equal(t, http.StatusAccepted, resp.Code)

	svc.EXPECT().ListBuilds(mock.Anything, 1, 20).Return(model.PaginatedBuilds{Builds: []model.Build{{ID: buildID, LogFilePath: "build-logs/demo.log", OwnerID: ownerID}}, TotalCount: 1}, nil)
	resp = perform(router, http.MethodGet, "/builds", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.NotContains(t, resp.Body.String(), "log_file_path")

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

	svc.EXPECT().GetSystemConfig(mock.Anything).Return(model.SystemConfig{BaseDomain: "example.test"}, nil)
	resp = perform(router, http.MethodGet, "/admin/config", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), "example.test")
	svc.EXPECT().UpdateSystemConfig(mock.Anything, mock.AnythingOfType("model.SystemConfig")).Return(nil)
	resp = perform(router, http.MethodPut, "/admin/config", `{"base_domain":"example.test"}`, nil)
	require.Equal(t, http.StatusOK, resp.Code)

	svc.EXPECT().GetUserStats(mock.Anything).Return(model.UserStats{ContainersTotal: 2}, nil)
	resp = perform(router, http.MethodGet, "/stats", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	svc.EXPECT().GetSystemMonitoring(mock.Anything).Return(model.SystemMonitoring{ContainersTotal: 3}, nil)
	resp = perform(router, http.MethodGet, "/admin/monitoring", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)

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
	router.POST("/builds/:id/cancel", h.CancelBuild)
	router.DELETE("/builds/:id", h.DeleteBuild)
	router.GET("/projects", h.GetProjects)
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
