package compose

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/gitsource"
	"github.com/google/uuid"
)

func TestStartGitDeploymentRejectsBuildWhenImageBuildsDisabled(t *testing.T) {
	repo := &composeProjectRepoFake{}
	o := newComposeOrchestratorForTest(context.Background(), repo)
	restore := stubGitClone(t, gitComposeWithBuild)
	defer restore()

	ctx := accessscope.WithUserScope(context.Background(), uuid.New(), "", "")
	_, err := o.StartGitDeployment(ctx, "proj", model.GitSource{RepoURL: "https://github.com/acme/app.git"})
	if !errors.Is(err, apperrors.ErrUnavailable) {
		t.Fatalf("StartGitDeployment() error = %v, want ErrUnavailable", err)
	}
	if repo.saved != 0 {
		t.Fatalf("saved projects = %d, want 0", repo.saved)
	}
}

func TestStartGitDeploymentAllowsImageOnlyComposeWhenImageBuildsDisabled(t *testing.T) {
	parentCtx, cancelParent := context.WithCancel(context.Background())
	cancelParent()
	repo := &composeProjectRepoFake{}
	o := newComposeOrchestratorForTest(parentCtx, repo)
	restore := stubGitClone(t, gitComposeWithoutBuild)
	defer restore()

	ctx := accessscope.WithUserScope(context.Background(), uuid.New(), "", "")
	projectID, err := o.StartGitDeployment(ctx, "proj", model.GitSource{RepoURL: "https://github.com/acme/app.git"})
	if err != nil {
		t.Fatalf("StartGitDeployment() error = %v", err)
	}
	if projectID == uuid.Nil {
		t.Fatalf("projectID is nil")
	}
	if repo.saved != 1 {
		t.Fatalf("saved projects = %d, want 1", repo.saved)
	}
	if err := o.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestStartDeploymentRejectsArchiveBuildWhenImageBuildsDisabled(t *testing.T) {
	repo := &composeProjectRepoFake{}
	o := newComposeOrchestratorForTest(context.Background(), repo)

	ctx := accessscope.WithUserScope(context.Background(), uuid.New(), "", "")
	archive := zipComposeArchive(t, gitComposeWithBuild)
	_, err := o.StartDeployment(ctx, "proj", "source.zip", bytes.NewReader(archive))
	if !errors.Is(err, apperrors.ErrUnavailable) {
		t.Fatalf("StartDeployment() error = %v, want ErrUnavailable", err)
	}
	if repo.saved != 0 {
		t.Fatalf("saved projects = %d, want 0", repo.saved)
	}
}

func newComposeOrchestratorForTest(parentCtx context.Context, repo *composeProjectRepoFake) *Orchestrator {
	return &Orchestrator{
		parentCtx:   parentCtx,
		parser:      NewParser(),
		projectRepo: repo,
		cfg: composeConfigFake{cfg: config.SystemConfig{
			GitSourcesEnabled:             true,
			GitAllowedHosts:               []string{"github.com"},
			GitCloneTimeoutSeconds:        1,
			GitMaxRepositoryBytes:         1024 * 1024,
			ImageBuildsEnabled:            false,
			ComposeUploadMaxBytes:         1024 * 1024,
			ComposePipelineTimeoutMinutes: 1,
		}},
		objectStore: &composeObjectStoreFake{objects: make(map[string][]byte)},
		active:      make(map[uuid.UUID]*deploymentState),
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func stubGitClone(t *testing.T, composeYAML string) func() {
	t.Helper()
	previous := cloneGitRepository
	cloneGitRepository = func(ctx context.Context, req gitsource.CloneRequest) (gitsource.RepoInfo, error) {
		if err := ctx.Err(); err != nil {
			return gitsource.RepoInfo{}, err
		}
		if err := os.MkdirAll(req.DestDir, 0755); err != nil {
			return gitsource.RepoInfo{}, err
		}
		if err := os.WriteFile(filepath.Join(req.DestDir, "docker-compose.yml"), []byte(composeYAML), 0644); err != nil {
			return gitsource.RepoInfo{}, err
		}
		return gitsource.RepoInfo{Host: "github.com", Path: "acme/app.git"}, nil
	}
	return func() {
		cloneGitRepository = previous
	}
}

type composeProjectRepoFake struct {
	saved int
}

func (f *composeProjectRepoFake) Save(ctx context.Context, p model.Project) error {
	f.saved++
	return nil
}

func (f *composeProjectRepoFake) CreateWithComposeDeploymentJob(ctx context.Context, p model.Project, job model.ComposeDeploymentJob, outbox model.ComposeDeploymentOutbox) error {
	f.saved++
	return nil
}

func (f *composeProjectRepoFake) GetByID(ctx context.Context, id uuid.UUID) (model.Project, error) {
	return model.Project{ID: id}, nil
}

func (f *composeProjectRepoFake) GetComposeDeploymentJob(ctx context.Context, id uuid.UUID) (model.ComposeDeploymentJob, error) {
	return model.ComposeDeploymentJob{ID: id}, nil
}

func (f *composeProjectRepoFake) GetActiveComposeDeploymentJobByProjectID(ctx context.Context, projectID uuid.UUID) (model.ComposeDeploymentJob, error) {
	return model.ComposeDeploymentJob{}, apperrors.ErrNotFound
}

func (f *composeProjectRepoFake) StartComposeDeploymentJob(ctx context.Context, id uuid.UUID) (model.ComposeDeploymentJob, bool, error) {
	return model.ComposeDeploymentJob{}, false, nil
}

func (f *composeProjectRepoFake) CompleteComposeDeploymentJob(ctx context.Context, id uuid.UUID, status string, errorMsg *string) error {
	return nil
}

func (f *composeProjectRepoFake) RequestComposeDeploymentCancel(ctx context.Context, projectID uuid.UUID) error {
	return nil
}

func (f *composeProjectRepoFake) UpdateStatus(ctx context.Context, id uuid.UUID, status string, errMsg *string) error {
	return nil
}

func (f *composeProjectRepoFake) SaveServiceGraph(ctx context.Context, projectID uuid.UUID, services []model.ProjectServiceNode) error {
	return nil
}

type composeConfigFake struct {
	cfg config.SystemConfig
}

func (f composeConfigFake) Get() config.SystemConfig {
	return f.cfg
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

type composeObjectStoreFake struct {
	objects map[string][]byte
}

func (f *composeObjectStoreFake) UploadStream(ctx context.Context, objectKey string, reader io.Reader, size int64, contentType string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	f.objects[objectKey] = data
	return nil
}

func (f *composeObjectStoreFake) DeleteObject(ctx context.Context, objectKey string) error {
	delete(f.objects, objectKey)
	return nil
}

func (f *composeObjectStoreFake) CopyObject(ctx context.Context, sourceKey, destKey, contentType string) error {
	f.objects[destKey] = append([]byte(nil), f.objects[sourceKey]...)
	return nil
}

func (f *composeObjectStoreFake) OpenObject(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	data, ok := f.objects[objectKey]
	if !ok {
		return nil, apperrors.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *composeObjectStoreFake) NewReaderAt(ctx context.Context, objectKey string) (io.ReaderAt, int64, error) {
	data, ok := f.objects[objectKey]
	if !ok {
		return nil, 0, apperrors.ErrNotFound
	}
	return bytes.NewReader(data), int64(len(data)), nil
}

func zipComposeArchive(t *testing.T, composeYAML string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(composeYAML)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
