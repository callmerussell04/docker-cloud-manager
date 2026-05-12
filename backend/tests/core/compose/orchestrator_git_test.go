package compose_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	. "github.com/callmerussell04/docker-cloud-manager/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	composemocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service/compose"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestStartDeploymentAllowsImageOnlyComposeWhenImageBuildsDisabled(t *testing.T) {
	repo := composemocks.NewProjectRepository(t)
	o := newComposeOrchestratorForTest(t, context.Background(), repo)
	var savedProject model.Project
	var savedJob model.ComposeDeploymentJob

	repo.EXPECT().
		CreateWithComposeDeploymentJob(mock.Anything, mock.AnythingOfType("model.Project"), mock.AnythingOfType("model.ComposeDeploymentJob"), mock.AnythingOfType("model.ComposeDeploymentOutbox")).
		Run(func(ctx context.Context, p model.Project, job model.ComposeDeploymentJob, outbox model.ComposeDeploymentOutbox) {
			savedProject = p
			savedJob = job
		}).
		Return(nil)

	ctx := accessscope.WithUserScope(context.Background(), uuid.New(), "", "")
	projectID, err := o.StartDeployment(ctx, "proj", "source.zip", bytes.NewReader(zipComposeArchive(t, gitComposeWithoutBuild)))
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, projectID)
	require.Equal(t, projectID, savedProject.ID)
	require.Equal(t, projectID, savedJob.ProjectID)
	require.Equal(t, model.ComposeDeploymentStatusQueued, savedJob.Status)
}

func TestStartDeploymentRejectsArchiveBuildWhenImageBuildsDisabled(t *testing.T) {
	repo := composemocks.NewProjectRepository(t)
	o := newComposeOrchestratorForTest(t, context.Background(), repo)

	ctx := accessscope.WithUserScope(context.Background(), uuid.New(), "", "")
	_, err := o.StartDeployment(ctx, "proj", "source.zip", bytes.NewReader(zipComposeArchive(t, gitComposeWithBuild)))
	require.ErrorIs(t, err, apperrors.ErrUnavailable)
	repo.AssertNotCalled(t, "CreateWithComposeDeploymentJob", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func newComposeOrchestratorForTest(t *testing.T, parentCtx context.Context, repo *composemocks.ProjectRepository) *Orchestrator {
	t.Helper()
	objectStore := composemocks.NewObjectStorage(t)
	objects := make(map[string][]byte)
	objectStore.EXPECT().
		UploadStream(mock.Anything, mock.AnythingOfType("string"), mock.Anything, int64(-1), "application/octet-stream").
		Run(func(ctx context.Context, objectKey string, reader io.Reader, size int64, contentType string) {
			data, err := io.ReadAll(reader)
			require.NoError(t, err)
			objects[objectKey] = data
		}).
		Return(nil).
		Maybe()
	objectStore.EXPECT().
		NewReaderAt(mock.Anything, mock.AnythingOfType("string")).
		RunAndReturn(func(ctx context.Context, objectKey string) (io.ReaderAt, int64, error) {
			data, ok := objects[objectKey]
			if !ok {
				return nil, 0, apperrors.ErrNotFound
			}
			return bytes.NewReader(data), int64(len(data)), nil
		}).
		Maybe()
	objectStore.EXPECT().
		DeleteObject(mock.Anything, mock.AnythingOfType("string")).
		Run(func(ctx context.Context, objectKey string) {
			delete(objects, objectKey)
		}).
		Return(nil).
		Maybe()
	cfg := composemocks.NewConfigProvider(t)
	cfg.EXPECT().Get().Return(config.SystemConfig{
		GitSourcesEnabled:             true,
		GitAllowedHosts:               []string{"github.com"},
		GitCloneTimeoutSeconds:        1,
		GitMaxRepositoryBytes:         1024 * 1024,
		ImageBuildsEnabled:            false,
		ComposeUploadMaxBytes:         1024 * 1024,
		ComposePipelineTimeoutMinutes: 1,
	}).Maybe()
	return NewOrchestrator(parentCtx, repo, nil, nil, nil, nil, cfg, objectStore, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

const gitComposeWithBuild = `
services:
  web:
    build:
      context: .
`

const gitComposeWithoutBuild = `
services:
  web:
    image: nginx:latest
`

func zipComposeArchive(t *testing.T, composeYAML string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("docker-compose.yml")
	require.NoError(t, err)
	_, err = w.Write([]byte(composeYAML))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}
