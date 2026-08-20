package compose_flow_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/composequeue"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	composemocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/coretest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestComposeUploadPersistsDurableJobAndPlan(t *testing.T) {
	ctx := context.Background()
	repos := coretest.OpenCoreRepositories(t)
	ownerID := uuid.New()
	objects := map[string][]byte{}
	orchestrator := newOrchestrator(t, repos, ownerID, objects, coretest.IntegrationConfig())

	projectID, err := orchestrator.StartDeployment(coretest.UserContext(ownerID), "demo", "compose.zip", "", bytes.NewReader(composeZip(t, map[string]string{
		"docker-compose.yml": "services:\n  web:\n    image: nginx:latest\n",
	})))
	require.NoError(t, err)

	project, err := repos.Projects.GetByID(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, ownerID, project.OwnerID)
	require.Equal(t, model.ProjectStatusBuilding, project.Status)

	job, err := repos.Projects.GetActiveComposeDeploymentJobByProjectID(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, model.ComposeDeploymentStatusQueued, job.Status)
	require.NotEmpty(t, job.SourceObjectKey)
	require.Contains(t, objects, job.SourceObjectKey)
	require.Len(t, mustComposeOutbox(t, repos.DB), 1)
	activeBytes, err := repos.Staged.ActiveBytesByOwner(ctx, ownerID)
	require.NoError(t, err)
	require.NotZero(t, activeBytes)

	require.NoError(t, orchestrator.HandleDeploymentMessage(ctx, composequeue.DeploymentMessage{
		JobID:     job.ID.String(),
		ProjectID: projectID.String(),
		OwnerID:   ownerID.String(),
		RequestID: "req-1",
	}))

	job, err = repos.Projects.GetComposeDeploymentJob(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, model.ComposeDeploymentStatusRunning, job.Status)
	require.Equal(t, model.ComposeDeploymentStageCreating, job.Stage)
	var plan struct {
		ProjectName string `json:"project_name"`
		Services    []struct {
			Name     string
			ImageTag string
		}
	}
	require.NoError(t, json.Unmarshal(job.PlanJSON, &plan))
	require.Equal(t, "demo", plan.ProjectName)
	require.Len(t, plan.Services, 1)
	require.Equal(t, "web", plan.Services[0].Name)
	require.Equal(t, "nginx:latest", plan.Services[0].ImageTag)
	require.NotEmpty(t, job.ResourceMapJSON)
	require.NotContains(t, objects, job.SourceObjectKey)
	activeBytes, err = repos.Staged.ActiveBytesByOwner(ctx, ownerID)
	require.NoError(t, err)
	require.Zero(t, activeBytes)
}

func TestComposeUploadAcceptsRequestedComposeFileFromArchive(t *testing.T) {
	tests := []struct {
		name        string
		archiveName string
		archive     []byte
		wantSuffix  string
	}{
		{
			name:        "zip",
			archiveName: "compose.zip",
			archive: composeZip(t, map[string]string{
				"deploy/docker-compose.yml": "services:\n  web:\n    image: nginx:latest\n",
			}),
			wantSuffix: ".zip",
		},
		{
			name:        "tar.gz",
			archiveName: "compose.tar.gz",
			archive: composeTarGz(t, map[string]string{
				"deploy/docker-compose.yml": "services:\n  web:\n    image: nginx:latest\n",
			}),
			wantSuffix: ".tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			repos := coretest.OpenCoreRepositories(t)
			ownerID := uuid.New()
			objects := map[string][]byte{}
			orchestrator := newOrchestrator(t, repos, ownerID, objects, coretest.IntegrationConfig())

			projectID, err := orchestrator.StartDeployment(coretest.UserContext(ownerID), "nested-demo", tt.archiveName, "deploy/docker-compose.yml", bytes.NewReader(tt.archive))
			require.NoError(t, err)

			job, err := repos.Projects.GetActiveComposeDeploymentJobByProjectID(ctx, projectID)
			require.NoError(t, err)
			require.Equal(t, "deploy/docker-compose.yml", job.ComposeFile)
			require.Equal(t, model.ComposeSourceTypeUpload, job.SourceType)
			require.True(t, strings.HasSuffix(job.SourceObjectKey, tt.wantSuffix), job.SourceObjectKey)
			require.Contains(t, objects, job.SourceObjectKey)
		})
	}
}

func TestComposeUploadWithBuildCreatesProjectBuildJob(t *testing.T) {
	ctx := context.Background()
	repos := coretest.OpenCoreRepositories(t)
	ownerID := uuid.New()
	objects := map[string][]byte{}
	orchestrator := newOrchestrator(t, repos, ownerID, objects, coretest.IntegrationConfig())

	projectID, err := orchestrator.StartDeployment(coretest.UserContext(ownerID), "build-demo", "compose.zip", "", bytes.NewReader(composeZip(t, map[string]string{
		"docker-compose.yml": "services:\n  web:\n    build:\n      context: .\n      dockerfile: Dockerfile\n",
		"Dockerfile":         "FROM scratch\n",
	})))
	require.NoError(t, err)
	job, err := repos.Projects.GetActiveComposeDeploymentJobByProjectID(ctx, projectID)
	require.NoError(t, err)

	require.NoError(t, orchestrator.HandleDeploymentMessage(ctx, composequeue.DeploymentMessage{JobID: job.ID.String(), ProjectID: projectID.String(), OwnerID: ownerID.String()}))

	builds, err := repos.Builds.GetByProjectID(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, builds, 1)
	require.Equal(t, projectID, *builds[0].ProjectID)
	require.Equal(t, "web", builds[0].ProjectServiceName)
	require.Equal(t, model.BuildStatusPending, builds[0].Status)

	job, err = repos.Projects.GetComposeDeploymentJob(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, model.ComposeDeploymentStageBuilding, job.Stage)
	require.Contains(t, string(job.ResourceMapJSON), builds[0].ID.String())
	require.NotContains(t, objects, job.SourceObjectKey)
}

func TestComposeCancelBeforeWorkerStartReleasesSource(t *testing.T) {
	ctx := context.Background()
	repos := coretest.OpenCoreRepositories(t)
	ownerID := uuid.New()
	objects := map[string][]byte{}
	orchestrator := newOrchestrator(t, repos, ownerID, objects, coretest.IntegrationConfig())

	projectID, err := orchestrator.StartDeployment(coretest.UserContext(ownerID), "cancel-demo", "compose.zip", "", bytes.NewReader(composeZip(t, map[string]string{
		"docker-compose.yml": "services:\n  web:\n    image: nginx:latest\n",
	})))
	require.NoError(t, err)
	job, err := repos.Projects.GetActiveComposeDeploymentJobByProjectID(ctx, projectID)
	require.NoError(t, err)
	require.NoError(t, repos.Projects.RequestComposeDeploymentCancel(ctx, projectID))

	require.NoError(t, orchestrator.HandleDeploymentMessage(ctx, composequeue.DeploymentMessage{JobID: job.ID.String(), ProjectID: projectID.String(), OwnerID: ownerID.String()}))

	job, err = repos.Projects.GetComposeDeploymentJob(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, model.ComposeDeploymentStatusCanceled, job.Status)
	project, err := repos.Projects.GetByID(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, model.ProjectStatusCanceled, project.Status)
	require.NotContains(t, objects, job.SourceObjectKey)
	activeBytes, err := repos.Staged.ActiveBytesByOwner(ctx, ownerID)
	require.NoError(t, err)
	require.Zero(t, activeBytes)
}

func TestComposeUploadValidationFailuresDoNotCreateDurableJob(t *testing.T) {
	tests := []struct {
		name        string
		cfgEdit     func(*config.SystemConfig)
		archive     []byte
		composeFile string
		wantErr     error
	}{
		{
			name:    "invalid compose",
			archive: composeZipForTable(t, map[string]string{"docker-compose.yml": "name: empty\n"}),
			wantErr: apperrors.ErrBadRequest,
		},
		{
			name:        "requested compose missing",
			archive:     composeZipForTable(t, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"}),
			composeFile: "deploy/docker-compose.yml",
			wantErr:     apperrors.ErrBadRequest,
		},
		{
			name:        "invalid requested compose path",
			archive:     composeZipForTable(t, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"}),
			composeFile: "../docker-compose.yml",
			wantErr:     apperrors.ErrBadRequest,
		},
		{
			name:        "raw compose with compose_file",
			archive:     []byte("services:\n  web:\n    image: nginx\n"),
			composeFile: "deploy/docker-compose.yml",
			wantErr:     apperrors.ErrBadRequest,
		},
		{
			name:    "corrupt tar",
			archive: []byte("not a tar"),
			wantErr: apperrors.ErrBadRequest,
		},
		{
			name: "disabled image builds with build service",
			cfgEdit: func(c *config.SystemConfig) {
				c.ImageBuildsEnabled = false
			},
			archive: composeZipForTable(t, map[string]string{
				"docker-compose.yml": "services:\n  web:\n    build: .\n",
				"Dockerfile":         "FROM scratch\n",
			}),
			wantErr: apperrors.ErrUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repos := coretest.OpenCoreRepositories(t)
			ownerID := uuid.New()
			objects := map[string][]byte{}
			cfg := coretest.IntegrationConfig()
			if tt.cfgEdit != nil {
				tt.cfgEdit(&cfg)
			}
			orchestrator := newOrchestrator(t, repos, ownerID, objects, cfg)
			archiveName := "compose.zip"
			if tt.name == "raw compose with compose_file" {
				archiveName = "docker-compose.yml"
			}
			if tt.name == "corrupt tar" {
				archiveName = "compose.tar"
			}
			_, err := orchestrator.StartDeployment(coretest.UserContext(ownerID), "bad-demo", archiveName, tt.composeFile, bytes.NewReader(tt.archive))
			require.ErrorIs(t, err, tt.wantErr)
			require.Zero(t, countTable(t, repos.DB, "projects"))
			require.Zero(t, countTable(t, repos.DB, "compose_deployment_jobs"))
			activeBytes, err := repos.Staged.ActiveBytesByOwner(context.Background(), ownerID)
			require.NoError(t, err)
			require.Zero(t, activeBytes)
		})
	}
}

func composeZipForTable(t *testing.T, files map[string]string) []byte {
	t.Helper()
	return composeZip(t, files)
}

func composeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	for name, body := range files {
		data := []byte(body)
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0600,
			Size: int64(len(data)),
		}))
		_, err := tw.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gzw.Close())
	return buf.Bytes()
}

func newOrchestrator(t *testing.T, repos coretest.CoreRepositories, ownerID uuid.UUID, objects map[string][]byte, cfgValue config.SystemConfig) *compose.Orchestrator {
	t.Helper()
	cfg := coretest.StaticConfig{Config: cfgValue}
	store := composemocks.NewObjectStorage(t)
	store.EXPECT().
		UploadStream(mock.Anything, mock.AnythingOfType("string"), mock.Anything, mock.AnythingOfType("int64"), mock.AnythingOfType("string")).
		RunAndReturn(func(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
			var buf bytes.Buffer
			_, err := io.Copy(&buf, r)
			if err != nil {
				return err
			}
			objects[key] = buf.Bytes()
			return nil
		}).
		Maybe()
	store.EXPECT().
		NewReaderAt(mock.Anything, mock.AnythingOfType("string")).
		RunAndReturn(func(ctx context.Context, key string) (io.ReaderAt, int64, error) {
			data, ok := objects[key]
			if !ok {
				return nil, 0, apperrors.ErrNotFound
			}
			return bytes.NewReader(data), int64(len(data)), nil
		}).
		Maybe()
	store.EXPECT().
		OpenObject(mock.Anything, mock.AnythingOfType("string")).
		RunAndReturn(func(ctx context.Context, key string) (io.ReadCloser, error) {
			data, ok := objects[key]
			if !ok {
				return nil, apperrors.ErrNotFound
			}
			return io.NopCloser(bytes.NewReader(data)), nil
		}).
		Maybe()
	store.EXPECT().
		CopyObject(mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string"), mock.AnythingOfType("string")).
		RunAndReturn(func(ctx context.Context, sourceKey, destKey, contentType string) error {
			data, ok := objects[sourceKey]
			if !ok {
				return apperrors.ErrNotFound
			}
			objects[destKey] = append([]byte(nil), data...)
			return nil
		}).
		Maybe()
	store.EXPECT().
		DeleteObject(mock.Anything, mock.AnythingOfType("string")).
		RunAndReturn(func(ctx context.Context, key string) error {
			delete(objects, key)
			return nil
		}).
		Maybe()

	users := coremocks.NewUserInfoProvider(t)
	users.EXPECT().
		GetUser(mock.Anything, ownerID).
		Return(model.UserInfo{ID: ownerID, Username: "composer", QuotaRAMMB: 4096, QuotaDiskMB: 4096, QuotaCPU: 4}, nil).
		Maybe()
	metrics := coremocks.NewHostDiskMetricsProvider(t)
	metrics.EXPECT().GetDiskUsage(mock.Anything).Return(model.HostDiskStats{FreeBytes: 1 << 40}, nil).Maybe()
	registry := coremocks.NewImageRegistryAPI(t)
	registry.EXPECT().GetImageSizeAndDigest(mock.Anything, mock.Anything, mock.Anything).Return(int64(1<<20), "sha256:digest", nil).Maybe()
	buildService := service.NewBuildService(repos.Builds, repos.Images, registry, users, coretest.DiscardLogger(), service.BuildServiceDeps{
		VolumeRepo:    repos.Volumes,
		Config:        cfg,
		ObjectStore:   store,
		StagedObjects: repos.Staged,
		DiskMetrics:   metrics,
		HostDiskPath:  "/",
	})
	orchestrator := compose.NewOrchestrator(
		context.Background(),
		repos.Projects,
		repos.Builds,
		composemocks.NewVolumeService(t),
		composemocks.NewContainerService(t),
		composemocks.NewComposeDockerAPI(t),
		cfg,
		store,
		buildService,
		coremocks.NewImageCleaner(t),
		coretest.DiscardLogger(),
	)
	orchestrator.SetResourceRepository(resourceRepository{containers: repos.Containers, volumes: repos.Volumes})
	orchestrator.SetStagedObjectRepository(repos.Staged)
	orchestrator.SetHostDiskGuard(metrics, "/")
	orchestrator.SetUserInfoProvider(users)
	return orchestrator
}

func composeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, contents := range files {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = io.Copy(w, bytes.NewBufferString(contents))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

type composeOutboxRow struct {
	Payload []byte
}

func mustComposeOutbox(t *testing.T, db *sql.DB) []composeOutboxRow {
	t.Helper()
	rows, err := db.Query(`SELECT payload FROM compose_deployment_queue_outbox ORDER BY created_at, id`)
	require.NoError(t, err)
	defer rows.Close()
	var out []composeOutboxRow
	for rows.Next() {
		var row composeOutboxRow
		require.NoError(t, rows.Scan(&row.Payload))
		var msg composequeue.DeploymentMessage
		require.NoError(t, json.Unmarshal(row.Payload, &msg))
		out = append(out, row)
	}
	require.NoError(t, rows.Err())
	return out
}

func countTable(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&count))
	return count
}

type resourceRepository struct {
	containers interface {
		GetByProjectID(context.Context, uuid.UUID) ([]model.Container, error)
	}
	volumes interface {
		GetByProjectID(context.Context, uuid.UUID) ([]model.Volume, error)
	}
}

func (r resourceRepository) GetByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Container, error) {
	return r.containers.GetByProjectID(ctx, projectID)
}

func (r resourceRepository) GetVolumesByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Volume, error) {
	return r.volumes.GetByProjectID(ctx, projectID)
}
