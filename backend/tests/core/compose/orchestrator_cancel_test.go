package compose_test

import (
	"context"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/composequeue"
	composemocks "github.com/callmerussell04/docker-cloud-manager/backend/tests/mocks/core/service/compose"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCancelDeploymentRequestsCancelForActiveProject(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := composemocks.NewProjectRepository(t)
	o := newComposeOrchestratorForTest(t, repo)
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
	o := newComposeOrchestratorForTest(t, repo)

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID, Status: model.ProjectStatusRunning}, nil)

	require.NoError(t, o.CancelDeploymentForBuild(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID, uuid.New()))
	repo.AssertNotCalled(t, "RequestComposeDeploymentCancel", mock.Anything, mock.Anything)
}

func TestCancelDeploymentRejectsInactiveProject(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := composemocks.NewProjectRepository(t)
	o := newComposeOrchestratorForTest(t, repo)

	repo.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID, Status: model.ProjectStatusRunning}, nil)

	err := o.CancelDeployment(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID)
	require.ErrorIs(t, err, apperrors.ErrConflict)
}

func TestHandleDeploymentMessageReturnsFailedCancellationWrites(t *testing.T) {
	jobErr := errors.New("complete job failed")
	projectErr := errors.New("update project failed")
	tests := []struct {
		name       string
		jobErr     error
		projectErr error
	}{
		{name: "recorded"},
		{name: "job status", jobErr: jobErr},
		{name: "project status", projectErr: projectErr},
		{name: "both", jobErr: jobErr, projectErr: projectErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := composemocks.NewProjectRepository(t)
			o := newComposeOrchestratorForTest(t, repo)
			job := model.ComposeDeploymentJob{ID: uuid.New(), ProjectID: uuid.New(), OwnerID: uuid.New(), CancelRequested: true}
			repo.EXPECT().GetComposeDeploymentJob(mock.Anything, job.ID).Return(job, nil)
			repo.EXPECT().CompleteComposeDeploymentJob(mock.Anything, job.ID, model.ComposeDeploymentStatusCanceled, mock.Anything).Return(tt.jobErr)
			repo.EXPECT().UpdateStatus(mock.Anything, job.ProjectID, model.ProjectStatusCanceled, mock.Anything).Return(tt.projectErr)

			err := o.HandleDeploymentMessage(context.Background(), composequeue.DeploymentMessage{JobID: job.ID.String()})
			if tt.jobErr == nil && tt.projectErr == nil {
				require.NoError(t, err)
				return
			}
			for _, want := range []error{tt.jobErr, tt.projectErr} {
				if want != nil {
					require.ErrorIs(t, err, want)
				}
			}
		})
	}
}
