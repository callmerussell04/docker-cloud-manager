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

func TestProjectServiceDeleteQueuesLifecycleRequest(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	containers := coremocks.NewProjectContainerLifecycle(t)
	volumes := coremocks.NewProjectVolumeLifecycle(t)
	svc := NewProjectService(repo, coremocks.NewProjectResourceRepository(t), coremocks.NewProjectDockerAPI(t), containers, volumes, staticConfig{})

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID}, nil)
	repo.EXPECT().UpdateStatus(mock.Anything, projectID, model.ProjectStatusDeleting, mock.Anything).Return(nil)

	require.NoError(t, svc.Delete(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID))
}

func TestProjectServiceDeleteQueuesLifecycleRequestForProjectsWithContainers(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	containers := coremocks.NewProjectContainerLifecycle(t)
	volumes := coremocks.NewProjectVolumeLifecycle(t)
	svc := NewProjectService(repo, coremocks.NewProjectResourceRepository(t), coremocks.NewProjectDockerAPI(t), containers, volumes, staticConfig{})

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID}, nil)
	repo.EXPECT().UpdateStatus(mock.Anything, projectID, model.ProjectStatusDeleting, mock.Anything).Return(nil)

	require.NoError(t, svc.Delete(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID))
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

func TestProjectServiceStartQueuesLifecycleRequest(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	containers := coremocks.NewProjectContainerLifecycle(t)
	svc := NewProjectService(repo, coremocks.NewProjectResourceRepository(t), coremocks.NewProjectDockerAPI(t), containers, coremocks.NewProjectVolumeLifecycle(t), staticConfig{})

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID}, nil)
	repo.EXPECT().UpdateStatus(mock.Anything, projectID, model.ProjectStatusStarting, mock.Anything).Return(nil)

	require.NoError(t, svc.Start(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID))
}

func TestProjectServiceStopQueuesLifecycleRequest(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	containers := coremocks.NewProjectContainerLifecycle(t)
	svc := NewProjectService(repo, coremocks.NewProjectResourceRepository(t), coremocks.NewProjectDockerAPI(t), containers, coremocks.NewProjectVolumeLifecycle(t), staticConfig{})

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID}, nil)
	repo.EXPECT().UpdateStatus(mock.Anything, projectID, model.ProjectStatusStopping, mock.Anything).Return(nil)

	require.NoError(t, svc.Stop(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID))
}

func TestProjectServiceStartRejectsBusyProject(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := coremocks.NewProjectRepository(t)
	containers := coremocks.NewProjectContainerLifecycle(t)
	svc := NewProjectService(repo, coremocks.NewProjectResourceRepository(t), coremocks.NewProjectDockerAPI(t), containers, coremocks.NewProjectVolumeLifecycle(t), staticConfig{})

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID, Status: model.ProjectStatusDeploying}, nil)

	err := svc.Start(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "project operation is already in progress")
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
