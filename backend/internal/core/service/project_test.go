package service_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestProjectServiceDeleteQueuesLifecycleRequest(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	svc := newTestProjectService(t, service.ProjectServiceDeps{Repository: repo})

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID}, nil)
	repo.EXPECT().UpdateStatus(mock.Anything, projectID, model.ProjectStatusDeleting, mock.Anything).Return(nil)

	require.NoError(t, svc.Delete(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID))
}

func TestProjectServiceCancelDelegatesDeploymentCanceler(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	canceler := coremocks.NewProjectDeploymentCanceler(t)
	svc := newTestProjectService(t, service.ProjectServiceDeps{Repository: repo})
	svc.SetDeploymentCanceler(canceler)
	var canceled uuid.UUID

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID, Status: model.ProjectStatusRunning}, nil)
	canceler.EXPECT().CancelDeployment(mock.Anything, projectID).Run(func(ctx context.Context, gotProjectID uuid.UUID) {
		canceled = gotProjectID
	}).Return(nil)

	require.NoError(t, svc.Cancel(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID))
	require.Equal(t, projectID, canceled)
}

func TestProjectServiceStartPreflightsStoppedContainersBeforeQueueing(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	resources := coremocks.NewProjectResourceRepository(t)
	capacity := coremocks.NewCapacityChecker(t)
	svc := newTestProjectService(t, service.ProjectServiceDeps{Repository: repo, Resources: resources, Capacity: capacity})

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID, Status: model.ProjectStatusStopped}, nil)
	resources.EXPECT().GetByProjectID(mock.Anything, projectID).Return([]model.Container{
		{ID: uuid.New(), OwnerID: ownerID, Status: model.ContainerStatusExited, BaseMemoryReservation: 256, BaseCPUReservation: 500},
		// Zero CPU reservation falls back to the configured default of 250.
		{ID: uuid.New(), OwnerID: ownerID, Status: model.ContainerStatusCreated, BaseMemoryReservation: 128},
		{ID: uuid.New(), OwnerID: ownerID, Status: model.ContainerStatusRunning, BaseMemoryReservation: 1024, BaseCPUReservation: 1000},
		{ID: uuid.New(), OwnerID: ownerID, Status: model.ContainerStatusStarting, BaseMemoryReservation: 1024, BaseCPUReservation: 1000},
		{ID: uuid.New(), OwnerID: ownerID, Status: model.ContainerStatusMissing, BaseMemoryReservation: 1024, BaseCPUReservation: 1000},
		{ID: uuid.New(), OwnerID: ownerID, Status: model.ContainerStatusError, BaseMemoryReservation: 1024, BaseCPUReservation: 1000},
	}, nil)
	capacity.EXPECT().CheckCapacity(mock.Anything, ownerID, int64(384), int64(750), int64(0)).Return(nil).Once()
	repo.EXPECT().UpdateStatus(mock.Anything, projectID, model.ProjectStatusStarting, (*string)(nil)).Return(nil).Once()

	require.NoError(t, svc.Start(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID))
}

func TestProjectServiceStartRejectsInsufficientCapacity(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	resources := coremocks.NewProjectResourceRepository(t)
	capacity := coremocks.NewCapacityChecker(t)
	svc := newTestProjectService(t, service.ProjectServiceDeps{Repository: repo, Resources: resources, Capacity: capacity})

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID}, nil)
	resources.EXPECT().GetByProjectID(mock.Anything, projectID).Return([]model.Container{
		{ID: uuid.New(), OwnerID: ownerID, Status: model.ContainerStatusExited, BaseMemoryReservation: 256},
	}, nil)
	capacity.EXPECT().CheckCapacity(mock.Anything, ownerID, int64(256), int64(250), int64(0)).Return(apperrors.ErrHostExhausted)

	err := svc.Start(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID)
	require.ErrorIs(t, err, apperrors.ErrHostExhausted)
}

func TestProjectServiceStartAllowsAdminAndChecksOwnerCapacity(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	resources := coremocks.NewProjectResourceRepository(t)
	capacity := coremocks.NewCapacityChecker(t)
	svc := newTestProjectService(t, service.ProjectServiceDeps{Repository: repo, Resources: resources, Capacity: capacity})

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID}, nil)
	resources.EXPECT().GetByProjectID(mock.Anything, projectID).Return([]model.Container{
		{ID: uuid.New(), OwnerID: ownerID, Status: model.ContainerStatusExited, BaseMemoryReservation: 64, BaseCPUReservation: 100},
	}, nil)
	capacity.EXPECT().CheckCapacity(mock.Anything, ownerID, int64(64), int64(100), int64(0)).Return(nil).Once()
	repo.EXPECT().UpdateStatus(mock.Anything, projectID, model.ProjectStatusStarting, (*string)(nil)).Return(nil).Once()

	require.NoError(t, svc.Start(accessscope.WithAdminScope(context.Background(), uuid.New(), "", ""), projectID))
}

func TestProjectServiceStartHidesOtherUsersProject(t *testing.T) {
	projectID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	svc := newTestProjectService(t, service.ProjectServiceDeps{Repository: repo})

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: uuid.New()}, nil)

	// Foreign projects are hidden rather than reported as forbidden.
	err := svc.Start(accessscope.WithUserScope(context.Background(), uuid.New(), "", ""), projectID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
}

func TestProjectServiceStopQueuesLifecycleRequest(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	svc := newTestProjectService(t, service.ProjectServiceDeps{Repository: repo})

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID}, nil)
	repo.EXPECT().UpdateStatus(mock.Anything, projectID, model.ProjectStatusStopping, mock.Anything).Return(nil)

	require.NoError(t, svc.Stop(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID))
}

func TestProjectServiceStartRejectsBusyProject(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	svc := newTestProjectService(t, service.ProjectServiceDeps{Repository: repo})

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID, Status: model.ProjectStatusDeploying}, nil)

	err := svc.Start(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID)
	require.ErrorIs(t, err, apperrors.ErrConflict)
	require.Contains(t, err.Error(), "project operation is already in progress")
}

func TestProjectLifecycleCoordinatorStartsServiceAfterHealthyDependency(t *testing.T) {
	f := newProjectStartFixture(t, false)
	f.dependencyHealth("healthy")
	f.containers.EXPECT().Start(mock.Anything, f.webID).Return(nil).Once()

	f.tick()
}

func TestProjectLifecycleCoordinatorWaitsForDependencyHealth(t *testing.T) {
	f := newProjectStartFixture(t, false)
	f.dependencyHealth("starting")

	f.tick()
}

func TestProjectLifecycleCoordinatorFailsProjectWhenRequiredDependencyFails(t *testing.T) {
	f := newProjectStartFixture(t, false)
	f.dependencyHealth("unhealthy")
	f.repo.EXPECT().UpdateStatus(mock.Anything, f.projectID, model.ProjectStatusFailed, mock.AnythingOfType("*string")).Return(nil).Once()

	f.tick()
}

func TestProjectLifecycleCoordinatorLogsFailedProjectStatusWrite(t *testing.T) {
	var logs bytes.Buffer
	f := newProjectStartFixtureWithLogger(t, false, slog.New(slog.NewJSONHandler(&logs, nil)))
	f.dependencyHealth("unhealthy")
	f.repo.EXPECT().UpdateStatus(mock.Anything, f.projectID, model.ProjectStatusFailed, mock.AnythingOfType("*string")).Return(errors.New("database unavailable")).Once()

	f.tick()
	require.Contains(t, logs.String(), `"msg":"failed to record project status"`)
	require.Contains(t, logs.String(), `"error":"database unavailable"`)
}

func TestProjectLifecycleCoordinatorContinuesWhenOptionalDependencyFails(t *testing.T) {
	f := newProjectStartFixture(t, true)
	f.dependencyHealth("unhealthy")
	f.containers.EXPECT().Start(mock.Anything, f.webID).Return(nil).Once()

	f.tick()
}

type projectStartFixture struct {
	t          *testing.T
	svc        *service.ProjectService
	repo       *coremocks.ProjectRepository
	containers *coremocks.ProjectContainerLifecycle
	docker     *coremocks.ProjectDockerAPI
	projectID  uuid.UUID
	webID      uuid.UUID
}

// newProjectStartFixture models a starting project whose db service runs and
// whose web service waits for db to become healthy.
func newProjectStartFixture(t *testing.T, optional bool) *projectStartFixture {
	t.Helper()
	return newProjectStartFixtureWithLogger(t, optional, nil)
}

func newProjectStartFixtureWithLogger(t *testing.T, optional bool, logger *slog.Logger) *projectStartFixture {
	t.Helper()
	f := &projectStartFixture{
		t:          t,
		repo:       coremocks.NewProjectRepository(t),
		containers: coremocks.NewProjectContainerLifecycle(t),
		docker:     coremocks.NewProjectDockerAPI(t),
		projectID:  uuid.New(),
		webID:      uuid.New(),
	}
	ownerID := uuid.New()
	dbID := uuid.New()
	f.svc = newTestProjectService(t, service.ProjectServiceDeps{Repository: f.repo, Containers: f.containers, Docker: f.docker, Logger: logger})

	f.repo.EXPECT().ListByStatuses(mock.Anything, mock.Anything, mock.Anything).
		Return([]model.Project{{ID: f.projectID, OwnerID: ownerID, Status: model.ProjectStatusStarting}}, nil).Once()
	f.repo.EXPECT().GetServiceGraph(mock.Anything, f.projectID).Return(dependencyGraph(f.projectID, dbID, f.webID, optional), nil).Once()
	f.containers.EXPECT().GetByID(mock.Anything, dbID).Return(model.Container{ID: dbID, OwnerID: ownerID, Status: model.ContainerStatusRunning, DockerID: "db-docker"}, nil)
	f.containers.EXPECT().GetByID(mock.Anything, f.webID).Return(model.Container{ID: f.webID, OwnerID: ownerID, Status: model.ContainerStatusCreated}, nil)
	return f
}

func (f *projectStartFixture) dependencyHealth(status string) {
	f.docker.EXPECT().InspectContainer(mock.Anything, "db-docker").Return(model.ContainerInspection{
		Healthcheck: &model.Healthcheck{},
		State:       model.ContainerState{Running: true, Status: "running", HealthStatus: &status},
	}, nil).Once()
}

// tick runs exactly one coordinator pass: the loop advances projects before
// waiting and returns once it observes the canceled context.
func (f *projectStartFixture) tick() {
	f.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.svc.RunLifecycleCoordinator(ctx)
}

func dependencyGraph(projectID, dbID, webID uuid.UUID, optional bool) []model.ProjectServiceNode {
	return []model.ProjectServiceNode{
		{
			ProjectID:   projectID,
			ContainerID: dbID,
			ServiceName: "db",
			StartOrder:  0,
		},
		{
			ProjectID:   projectID,
			ContainerID: webID,
			ServiceName: "web",
			StartOrder:  1,
			Dependencies: []model.ProjectServiceDependency{
				{
					ProjectID:            projectID,
					ContainerID:          webID,
					DependsOnContainerID: dbID,
					DependsOnServiceName: "db",
					Condition:            model.ComposeDependencyConditionHealthy,
					Optional:             optional,
				},
			},
		},
	}
}
