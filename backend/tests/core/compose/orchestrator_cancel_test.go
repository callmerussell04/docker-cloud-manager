package compose_test

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	composemocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service/compose"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCancelDeploymentRequestsCancelForActiveProject(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := composemocks.NewProjectRepository(t)
	o := newComposeOrchestratorForTest(t, context.Background(), repo)
	var updatedStatus string
	var cancelRequests int

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID, Status: model.ProjectStatusDeploying}, nil)
	repo.EXPECT().
		UpdateStatus(mock.Anything, projectID, model.ProjectStatusCanceling, mock.Anything).
		Run(func(ctx context.Context, id uuid.UUID, status string, errMsg *string) {
			updatedStatus = status
		}).
		Return(nil)
	repo.EXPECT().RequestComposeDeploymentCancel(mock.Anything, projectID).Run(func(ctx context.Context, id uuid.UUID) {
		cancelRequests++
	}).Return(nil)

	require.NoError(t, o.CancelDeployment(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID))
	require.Equal(t, model.ProjectStatusCanceling, updatedStatus)
	require.Equal(t, 1, cancelRequests)
}

func TestCancelDeploymentForBuildIgnoresInactiveProject(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := composemocks.NewProjectRepository(t)
	o := newComposeOrchestratorForTest(t, context.Background(), repo)

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID, Status: model.ProjectStatusRunning}, nil)

	require.NoError(t, o.CancelDeploymentForBuild(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID, uuid.New()))
	repo.AssertNotCalled(t, "RequestComposeDeploymentCancel", mock.Anything, mock.Anything)
}

func TestCancelDeploymentRejectsInactiveProject(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := composemocks.NewProjectRepository(t)
	o := newComposeOrchestratorForTest(t, context.Background(), repo)

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID, Status: model.ProjectStatusRunning}, nil)

	err := o.CancelDeployment(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID)
	require.ErrorIs(t, err, apperrors.ErrConflict)
}
