package compose_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	composemocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service/compose"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// A transient failure after the job starts but before its plan is durable
// returns the job to queued, so the redelivered message runs ingestion again.
func TestHandleDeploymentMessageRetriesTransientFailureBeforePlanIsDurable(t *testing.T) {
	transient := apperrors.Wrap(apperrors.ErrUnavailable, "database unavailable", errors.New("connection reset"))
	for _, tc := range []struct {
		name        string
		projectErr  error
		sourceErr   error
		planSaveErr error
	}{
		{name: "project read fails", projectErr: transient},
		{name: "source read fails", sourceErr: errors.New("object storage connection reset")},
		{name: "plan write fails", planSaveErr: transient},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := composemocks.NewProjectRepository(t)
			deps, objects := composeOrchestratorDeps(t, repo, composeTestConfig())
			source := &failingSourceStore{ObjectStorage: deps.ObjectStore, errs: []error{tc.sourceErr}}
			deps.ObjectStore = source
			o, err := compose.NewOrchestrator(deps)
			require.NoError(t, err)

			store := newComposeJobStore(repo, model.ComposeDeploymentJob{
				ID:              uuid.New(),
				ProjectID:       uuid.New(),
				OwnerID:         uuid.New(),
				SourceType:      model.ComposeSourceTypeUpload,
				SourceObjectKey: "compose-sources/demo.zip",
				Status:          model.ComposeDeploymentStatusQueued,
			})
			objects[store.job.SourceObjectKey] = zipComposeArchive(t, gitComposeWithoutBuild)
			store.projectErrs = []error{tc.projectErr}
			store.planSaveErrs = []error{tc.planSaveErr}
			msg := queue.DeploymentMessage{JobID: store.job.ID.String(), ProjectID: store.job.ProjectID.String()}

			err = o.HandleDeploymentMessage(context.Background(), msg)
			wantErr := transient
			if tc.sourceErr != nil {
				wantErr = tc.sourceErr
			}
			require.ErrorIs(t, err, wantErr, "the message stays retryable")
			require.Contains(t, objects, store.job.SourceObjectKey, "the source survives for the retry")
			require.Equal(t, model.ComposeDeploymentStatusQueued, store.job.Status)
			require.Empty(t, store.job.Stage)

			require.NoError(t, o.HandleDeploymentMessage(context.Background(), msg))
			require.Equal(t, model.ComposeDeploymentStatusRunning, store.job.Status)
			require.Equal(t, model.ComposeDeploymentStageCreating, store.job.Stage)
			require.NotEmpty(t, store.job.PlanJSON)
			require.Equal(t, model.ProjectStatusDeploying, store.projectStatus)
		})
	}
}

// composeJobStore keeps one deployment job's durable state behind the
// generated ProjectRepository mock. Each queued error fails the matching call
// once.
type composeJobStore struct {
	job           model.ComposeDeploymentJob
	projectStatus string
	projectErrs   []error
	planSaveErrs  []error
}

func newComposeJobStore(repo *composemocks.ProjectRepository, job model.ComposeDeploymentJob) *composeJobStore {
	s := &composeJobStore{job: job}
	repo.EXPECT().GetComposeDeploymentJob(mock.Anything, job.ID).RunAndReturn(func(context.Context, uuid.UUID) (model.ComposeDeploymentJob, error) {
		return s.job, nil
	}).Maybe()
	repo.EXPECT().StartComposeDeploymentJob(mock.Anything, job.ID).RunAndReturn(func(context.Context, uuid.UUID) (model.ComposeDeploymentJob, bool, error) {
		if s.job.Status != model.ComposeDeploymentStatusQueued {
			return s.job, false, nil
		}
		s.job.Status = model.ComposeDeploymentStatusRunning
		s.job.Attempts++
		return s.job, true, nil
	}).Maybe()
	repo.EXPECT().RequeueComposeDeploymentJob(mock.Anything, job.ID).RunAndReturn(func(context.Context, uuid.UUID) error {
		if s.job.Status == model.ComposeDeploymentStatusRunning {
			s.job.Status = model.ComposeDeploymentStatusQueued
		}
		return nil
	}).Maybe()
	repo.EXPECT().GetByID(mock.Anything, job.ProjectID).RunAndReturn(func(context.Context, uuid.UUID) (model.Project, error) {
		if err := popErr(&s.projectErrs); err != nil {
			return model.Project{}, err
		}
		return model.Project{ID: job.ProjectID, OwnerID: job.OwnerID, Name: "demo", Status: model.ProjectStatusBuilding}, nil
	}).Maybe()
	repo.EXPECT().SaveComposeDeploymentPlan(mock.Anything, job.ID, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, _ uuid.UUID, stage string, planJSON, resourceJSON []byte) error {
		if err := popErr(&s.planSaveErrs); err != nil {
			return err
		}
		s.job.Stage = stage
		s.job.PlanJSON = planJSON
		s.job.ResourceMapJSON = resourceJSON
		return nil
	}).Maybe()
	repo.EXPECT().UpdateStatus(mock.Anything, job.ProjectID, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, _ uuid.UUID, status string, _ *string) error {
		s.projectStatus = status
		return nil
	}).Maybe()
	return s
}

// failingSourceStore fails source reads with each queued error once.
type failingSourceStore struct {
	compose.ObjectStorage
	errs []error
}

func (s *failingSourceStore) NewReaderAt(ctx context.Context, objectKey string) (io.ReaderAt, int64, error) {
	if err := popErr(&s.errs); err != nil {
		return nil, 0, err
	}
	return s.ObjectStorage.NewReaderAt(ctx, objectKey)
}

func popErr(errs *[]error) error {
	if len(*errs) == 0 {
		return nil
	}
	err := (*errs)[0]
	*errs = (*errs)[1:]
	return err
}
