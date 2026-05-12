package service_test

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	. "github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestProjectServiceDeleteDelegatesNetworkCleanup(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	containerID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	resources := coremocks.NewProjectResourceRepository(t)
	containers := newProjectContainerMock(t)
	volumes := coremocks.NewProjectVolumeLifecycle(t)
	svc := NewProjectService(repo, resources, coremocks.NewProjectDockerAPI(t), containers, volumes, staticConfig{})
	var cleaned []uuid.UUID

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID}, nil)
	repo.EXPECT().UpdateStatus(mock.Anything, projectID, model.ProjectStatusDeleting, mock.Anything).Return(nil)
	resources.EXPECT().GetByProjectID(mock.Anything, projectID).Return([]model.Container{{ID: containerID, OwnerID: ownerID, ProjectID: &projectID, DockerID: "docker-project"}}, nil)
	containers.ProjectContainerLifecycle.EXPECT().Delete(mock.Anything, containerID).Return(nil)
	resources.EXPECT().GetVolumesByProjectID(mock.Anything, projectID).Return([]model.Volume(nil), nil)
	repo.EXPECT().Delete(mock.Anything, projectID).Return(nil)
	containers.ProjectNetworkCleaner.EXPECT().CleanupUserNetworkIfUnused(mock.Anything, ownerID).Run(func(ctx context.Context, gotOwnerID uuid.UUID) {
		cleaned = append(cleaned, gotOwnerID)
	}).Return(nil)

	require.NoError(t, svc.Delete(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID))
	require.Equal(t, []uuid.UUID{ownerID}, cleaned)
}

func TestProjectServiceDeleteDelegatesNetworkCleanupEvenWhenContainersRemain(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	otherProjectID := uuid.New()
	projectContainerID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	resources := coremocks.NewProjectResourceRepository(t)
	containers := newProjectContainerMock(t)
	volumes := coremocks.NewProjectVolumeLifecycle(t)
	svc := NewProjectService(repo, resources, coremocks.NewProjectDockerAPI(t), containers, volumes, staticConfig{})
	var cleanupCount int

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID}, nil)
	repo.EXPECT().UpdateStatus(mock.Anything, projectID, model.ProjectStatusDeleting, mock.Anything).Return(nil)
	_ = otherProjectID
	resources.EXPECT().GetByProjectID(mock.Anything, projectID).Return([]model.Container{
		{ID: projectContainerID, OwnerID: ownerID, ProjectID: &projectID, DockerID: "docker-project"},
	}, nil)
	containers.ProjectContainerLifecycle.EXPECT().Delete(mock.Anything, projectContainerID).Return(nil)
	resources.EXPECT().GetVolumesByProjectID(mock.Anything, projectID).Return([]model.Volume(nil), nil)
	repo.EXPECT().Delete(mock.Anything, projectID).Return(nil)
	containers.ProjectNetworkCleaner.EXPECT().CleanupUserNetworkIfUnused(mock.Anything, ownerID).Run(func(ctx context.Context, gotOwnerID uuid.UUID) {
		cleanupCount++
	}).Return(nil)

	require.NoError(t, svc.Delete(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID))
	require.Equal(t, 1, cleanupCount)
}

func TestProjectServiceCancelDelegatesDeploymentCanceler(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	canceler := coremocks.NewProjectDeploymentCanceler(t)
	svc := NewProjectService(repo, coremocks.NewProjectResourceRepository(t), coremocks.NewProjectDockerAPI(t), coremocks.NewProjectContainerLifecycle(t), coremocks.NewProjectVolumeLifecycle(t), staticConfig{})
	svc.SetDeploymentCanceler(canceler)
	var canceled uuid.UUID

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID, Status: model.ProjectStatusRunning}, nil)
	canceler.EXPECT().CancelDeployment(mock.Anything, projectID).Run(func(ctx context.Context, gotProjectID uuid.UUID) {
		canceled = gotProjectID
	}).Return(nil)

	require.NoError(t, svc.Cancel(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID))
	require.Equal(t, projectID, canceled)
}

func TestProjectServiceStartFailsWhenRequiredDependencyFails(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	dbID := uuid.New()
	webID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	dockerAPI := coremocks.NewProjectDockerAPI(t)
	containers := coremocks.NewProjectContainerLifecycle(t)
	svc := NewProjectService(repo, coremocks.NewProjectResourceRepository(t), dockerAPI, containers, coremocks.NewProjectVolumeLifecycle(t), staticConfig{})

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID}, nil)
	repo.EXPECT().GetServiceGraph(mock.Anything, projectID).Return(dependencyGraph(projectID, dbID, webID, false), nil)
	repo.EXPECT().UpdateStatus(mock.Anything, projectID, model.ProjectStatusStarting, mock.Anything).Return(nil)
	containers.EXPECT().GetByID(mock.Anything, dbID).Return(model.Container{ID: dbID, DockerID: "db-docker", Status: model.ContainerStatusCreated}, nil).Once()
	containers.EXPECT().Start(mock.Anything, dbID).Return(nil).Once()
	containers.EXPECT().GetByID(mock.Anything, dbID).Return(model.Container{ID: dbID, DockerID: "db-docker"}, nil).Once()
	dockerAPI.EXPECT().InspectContainer(mock.Anything, "db-docker").Return(model.ContainerInspection{}, nil)
	repo.EXPECT().UpdateStatus(mock.Anything, projectID, model.ProjectStatusFailed, mock.Anything).Return(nil)

	err := svc.Start(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "dependency db failed condition service_healthy")
	containers.AssertNotCalled(t, "Start", mock.Anything, webID)
}

func TestProjectServiceStartContinuesWhenOptionalDependencyFails(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	dbID := uuid.New()
	webID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	dockerAPI := coremocks.NewProjectDockerAPI(t)
	containers := coremocks.NewProjectContainerLifecycle(t)
	svc := NewProjectService(repo, coremocks.NewProjectResourceRepository(t), dockerAPI, containers, coremocks.NewProjectVolumeLifecycle(t), staticConfig{})
	var started []uuid.UUID

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID}, nil)
	repo.EXPECT().GetServiceGraph(mock.Anything, projectID).Return(dependencyGraph(projectID, dbID, webID, true), nil)
	repo.EXPECT().UpdateStatus(mock.Anything, projectID, model.ProjectStatusStarting, mock.Anything).Return(nil)
	containers.EXPECT().GetByID(mock.Anything, dbID).Return(model.Container{ID: dbID, DockerID: "db-docker"}, nil)
	dockerAPI.EXPECT().InspectContainer(mock.Anything, "db-docker").Return(model.ContainerInspection{}, nil)
	containers.EXPECT().GetByID(mock.Anything, dbID).Return(model.Container{ID: dbID, DockerID: "db-docker", Status: model.ContainerStatusCreated}, nil)
	containers.EXPECT().Start(mock.Anything, dbID).Run(func(ctx context.Context, id uuid.UUID) { started = append(started, id) }).Return(nil)
	containers.EXPECT().GetByID(mock.Anything, webID).Return(model.Container{ID: webID, DockerID: "web-docker", Status: model.ContainerStatusCreated}, nil)
	containers.EXPECT().Start(mock.Anything, webID).Run(func(ctx context.Context, id uuid.UUID) { started = append(started, id) }).Return(nil)
	repo.EXPECT().UpdateStatus(mock.Anything, projectID, model.ProjectStatusRunning, mock.Anything).Return(nil)

	require.NoError(t, svc.Start(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID))
	require.Contains(t, started, webID)
}

func TestProjectServiceStartTreatsZeroValueDependencyAsRequired(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	dbID := uuid.New()
	webID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	dockerAPI := coremocks.NewProjectDockerAPI(t)
	containers := coremocks.NewProjectContainerLifecycle(t)
	svc := NewProjectService(repo, coremocks.NewProjectResourceRepository(t), dockerAPI, containers, coremocks.NewProjectVolumeLifecycle(t), staticConfig{})

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID}, nil)
	repo.EXPECT().GetServiceGraph(mock.Anything, projectID).Return([]model.ProjectServiceNode{
		{ProjectID: projectID, ContainerID: dbID, ServiceName: "db", StartOrder: 0},
		{ProjectID: projectID, ContainerID: webID, ServiceName: "web", StartOrder: 1, Dependencies: []model.ProjectServiceDependency{{
			ProjectID:            projectID,
			ContainerID:          webID,
			DependsOnContainerID: dbID,
			DependsOnServiceName: "db",
			Condition:            model.ComposeDependencyConditionHealthy,
		}}},
	}, nil)
	repo.EXPECT().UpdateStatus(mock.Anything, projectID, model.ProjectStatusStarting, mock.Anything).Return(nil)
	containers.EXPECT().GetByID(mock.Anything, dbID).Return(model.Container{ID: dbID, DockerID: "db-docker", Status: model.ContainerStatusCreated}, nil).Once()
	containers.EXPECT().Start(mock.Anything, dbID).Return(nil).Once()
	containers.EXPECT().GetByID(mock.Anything, dbID).Return(model.Container{ID: dbID, DockerID: "db-docker"}, nil).Once()
	dockerAPI.EXPECT().InspectContainer(mock.Anything, "db-docker").Return(model.ContainerInspection{}, nil)
	repo.EXPECT().UpdateStatus(mock.Anything, projectID, model.ProjectStatusFailed, mock.Anything).Return(nil)

	err := svc.Start(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID)
	require.Error(t, err)
	containers.AssertNotCalled(t, "Start", mock.Anything, webID)
}

type projectContainerMock struct {
	*coremocks.ProjectContainerLifecycle
	*coremocks.ProjectNetworkCleaner
}

func newProjectContainerMock(t *testing.T) *projectContainerMock {
	t.Helper()
	return &projectContainerMock{
		ProjectContainerLifecycle: coremocks.NewProjectContainerLifecycle(t),
		ProjectNetworkCleaner:     coremocks.NewProjectNetworkCleaner(t),
	}
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
