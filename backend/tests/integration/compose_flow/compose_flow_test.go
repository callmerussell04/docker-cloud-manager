package compose_flow_test

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"strings"
	"sync"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const webCompose = "services:\n  web:\n    image: nginx:latest\n"

// stackCompose is a database with a healthcheck and a managed volume, and a
// web service that waits for the database to be healthy.
const stackCompose = `
services:
  db:
    image: postgres:17-alpine
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
    volumes:
      - data:/var/lib/postgresql/data
  web:
    image: nginx:latest
    depends_on:
      db:
        condition: service_healthy
volumes:
  data: {}
`

func TestComposeDeploymentRunsToDone(t *testing.T) {
	ctx := context.Background()
	cfg := coretest.IntegrationConfig()
	cfg.DefaultVolumeSizeBytes = 256 << 20
	f := coretest.NewComposeFixture(t, cfg)
	docker := expectStackDocker(t, f)

	projectID, err := f.Orchestrator.StartDeployment(f.OwnerContext(), "stack", "compose.zip", "", bytes.NewReader(coretest.ComposeZip(t, map[string]string{
		"docker-compose.yml": stackCompose,
	})))
	require.NoError(t, err)
	job, err := f.Repos.Projects.GetActiveComposeDeploymentJobByProjectID(ctx, projectID)
	require.NoError(t, err)

	// Plan: an image-only deployment goes straight to creating.
	require.NoError(t, f.Deliver(t))
	requireDeployment(t, f, job.ID, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStageCreating, model.ProjectStatusDeploying)

	// Create: the managed volume is queued first, and containers wait for it.
	f.Tick(t)
	volumes, err := f.Repos.Volumes.GetByProjectID(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, volumes, 1)
	require.Equal(t, model.VolumeStatusCreating, volumes[0].Status)
	require.EqualValues(t, 256<<20, volumes[0].SizeBytes, "a Compose volume gets the default size")
	require.False(t, volumes[0].SizeEnforced)
	require.Empty(t, stackContainers(t, f, projectID))
	require.Equal(t, 1, f.RunResourceOperations(t))

	f.Tick(t)
	containers := stackContainers(t, f, projectID)
	require.Len(t, containers, 2)
	require.Equal(t, model.ContainerStatusPending, containers["db"].Status)
	require.Equal(t, model.ContainerStatusPending, containers["web"].Status)
	require.Equal(t, 2, f.RunContainerOperations(t))

	f.Tick(t)
	requireDeployment(t, f, job.ID, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStageStarting, model.ProjectStatusDeploying)
	graph, err := f.Repos.Projects.GetServiceGraph(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, graph, 2)
	require.Equal(t, "db", graph[0].ServiceName)
	require.Equal(t, "web", graph[1].ServiceName)
	require.Equal(t, []model.ProjectServiceDependency{{
		ProjectID:            projectID,
		ContainerID:          containers["web"].ID,
		DependsOnContainerID: containers["db"].ID,
		DependsOnServiceName: "db",
		Condition:            model.ComposeDependencyConditionHealthy,
	}}, graph[1].Dependencies)

	// Wait for dependencies: the database starts; web waits for its health.
	f.Tick(t)
	containers = stackContainers(t, f, projectID)
	require.Equal(t, model.ContainerStatusStarting, containers["db"].Status)
	require.Equal(t, model.ContainerStatusCreated, containers["web"].Status)
	require.Equal(t, 1, f.RunContainerOperations(t))

	// Start: web starts once the database is healthy.
	f.Tick(t)
	containers = stackContainers(t, f, projectID)
	require.Equal(t, model.ContainerStatusRunning, containers["db"].Status)
	require.Equal(t, model.ContainerStatusStarting, containers["web"].Status)
	require.Equal(t, 1, f.RunContainerOperations(t))

	f.Tick(t)
	requireDeployment(t, f, job.ID, model.ComposeDeploymentStatusSucceeded, model.ComposeDeploymentStageStarting, model.ProjectStatusRunning)
	job, err = f.Repos.Projects.GetComposeDeploymentJob(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, job.FinishedAt)
	require.Nil(t, job.ErrorMessage)
	containers = stackContainers(t, f, projectID)
	require.Equal(t, model.ContainerStatusRunning, containers["db"].Status)
	require.Equal(t, model.ContainerStatusRunning, containers["web"].Status)
	volume, err := f.Repos.Volumes.GetByID(ctx, volumes[0].ID)
	require.NoError(t, err)
	require.Equal(t, model.VolumeStatusAvailable, volume.Status)
	require.False(t, f.Objects.Has(job.SourceObjectKey))
	activeBytes, err := f.Repos.Staged.ActiveBytesByOwner(ctx, f.OwnerID)
	require.NoError(t, err)
	require.Zero(t, activeBytes)
	active, err := f.Repos.Projects.ListActiveComposeDeploymentJobs(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, active, "a finished deployment leaves the coordinator")

	// Docker received the project's runtime specs.
	specs := docker.specs()
	require.Len(t, specs, 2)
	require.Equal(t, "postgres:17-alpine", specs["db"].ImageName)
	require.Equal(t, []model.ContainerMountSpec{{VolumeName: volume.DockerName, Target: "/var/lib/postgresql/data"}}, specs["db"].VolumeMounts)
	require.Equal(t, "nginx:latest", specs["web"].ImageName)
	for _, spec := range specs {
		require.Equal(t, projectID.String(), spec.ProjectID)
		require.Equal(t, f.OwnerID.String(), spec.OwnerID)
	}
}

// A Compose volume is admitted like any other: when the default size does not
// fit the owner's 4096 MB disk quota, the deployment fails with the quota
// error and creates nothing in Docker.
func TestComposeDeploymentFailsWhenItsVolumesDoNotFitTheDiskQuota(t *testing.T) {
	ctx := context.Background()
	cfg := coretest.IntegrationConfig()
	cfg.DefaultVolumeSizeBytes = 5 << 30
	f := coretest.NewComposeFixture(t, cfg)

	projectID, err := f.Orchestrator.StartDeployment(f.OwnerContext(), "stack", "compose.zip", "", bytes.NewReader(coretest.ComposeZip(t, map[string]string{
		"docker-compose.yml": stackCompose,
	})))
	require.NoError(t, err)
	job, err := f.Repos.Projects.GetActiveComposeDeploymentJobByProjectID(ctx, projectID)
	require.NoError(t, err)
	require.NoError(t, f.Deliver(t))

	for range 3 {
		f.Tick(t)
		if job, err = f.Repos.Projects.GetComposeDeploymentJob(ctx, job.ID); err == nil && job.Status == model.ComposeDeploymentStatusFailed {
			break
		}
	}

	require.NoError(t, err)
	require.Equal(t, model.ComposeDeploymentStatusFailed, job.Status)
	require.NotNil(t, job.ErrorMessage)
	require.Contains(t, *job.ErrorMessage, "user disk quota exceeded")
	project, err := f.Repos.Projects.GetByID(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, model.ProjectStatusFailed, project.Status)
	volumes, _, err := f.Repos.Volumes.List(ctx, model.ListOptions{OwnerID: &f.OwnerID})
	require.NoError(t, err)
	require.Empty(t, volumes, "no volume is recorded")
	require.Empty(t, stackContainers(t, f, projectID))
}

// stackDocker records the runtime specs Docker is asked to create.
type stackDocker struct {
	mu      sync.Mutex
	created map[string]model.ContainerRuntimeSpec
}

func (d *stackDocker) specs() map[string]model.ContainerRuntimeSpec {
	d.mu.Lock()
	defer d.mu.Unlock()
	return maps.Clone(d.created)
}

// expectStackDocker allows exactly the Docker calls that deploying
// stackCompose makes: one volume, two containers each created and started
// once, and two health checks of the database, the first still starting.
func expectStackDocker(t *testing.T, f *coretest.ComposeFixture) *stackDocker {
	t.Helper()
	docker := &stackDocker{created: make(map[string]model.ContainerRuntimeSpec)}
	f.VolumeDocker.EXPECT().CreateVolume(mock.Anything, mock.AnythingOfType("model.VolumeRuntimeSpec")).Return("docker-volume", nil).Once()
	f.ContainerDocker.EXPECT().InspectVolume(mock.Anything, mock.Anything).Return(model.VolumeInspection{}, nil).Once()
	f.ContainerDocker.EXPECT().EnsureUserNetwork(mock.Anything, mock.Anything).Return("network", nil).Times(2)
	f.ContainerDocker.EXPECT().ImageExists(mock.Anything, mock.Anything).Return(true, nil).Times(2)
	f.ContainerDocker.EXPECT().CreateContainer(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, spec model.ContainerRuntimeSpec) (string, error) {
			docker.mu.Lock()
			defer docker.mu.Unlock()
			docker.created[spec.NetworkAlias] = spec
			return "docker-" + spec.NetworkAlias, nil
		}).Times(2)
	// Each start applies the container's resources before Docker runs it.
	f.ContainerDocker.EXPECT().UpdateContainerResources(mock.Anything, "docker-db", mock.Anything).Return(nil).Once()
	f.ContainerDocker.EXPECT().UpdateContainerResources(mock.Anything, "docker-web", mock.Anything).Return(nil).Once()
	f.ContainerDocker.EXPECT().StartContainer(mock.Anything, "docker-db").Return(nil).Once()
	f.ContainerDocker.EXPECT().StartContainer(mock.Anything, "docker-web").Return(nil).Once()
	f.ContainerDocker.EXPECT().InspectContainer(mock.Anything, "docker-db").Return(dbInspection("starting"), nil).Once()
	f.ContainerDocker.EXPECT().InspectContainer(mock.Anything, "docker-db").Return(dbInspection("healthy"), nil).Once()
	return docker
}

func dbInspection(health string) model.ContainerInspection {
	return model.ContainerInspection{
		Healthcheck: &model.Healthcheck{Test: []string{"CMD-SHELL", "pg_isready -U postgres"}},
		State:       model.ContainerState{Status: "running", Running: true, HealthStatus: &health},
	}
}

func requireDeployment(t *testing.T, f *coretest.ComposeFixture, jobID uuid.UUID, status, stage, projectStatus string) {
	t.Helper()
	job, err := f.Repos.Projects.GetComposeDeploymentJob(context.Background(), jobID)
	require.NoError(t, err)
	require.Equal(t, status, job.Status, "job status")
	require.Equal(t, stage, job.Stage, "job stage")
	project, err := f.Repos.Projects.GetByID(context.Background(), job.ProjectID)
	require.NoError(t, err)
	require.Equal(t, projectStatus, project.Status, "project status")
}

// stackContainers returns the containers of a project named "stack" by
// Compose service.
func stackContainers(t *testing.T, f *coretest.ComposeFixture, projectID uuid.UUID) map[string]model.Container {
	t.Helper()
	containers, err := f.Repos.Containers.GetByProjectID(context.Background(), projectID)
	require.NoError(t, err)
	byService := make(map[string]model.Container, len(containers))
	for _, c := range containers {
		byService[strings.TrimPrefix(c.Name, "stack_")] = c
	}
	return byService
}

func TestComposeUploadPersistsDurableJobAndPlan(t *testing.T) {
	ctx := context.Background()
	f := coretest.NewComposeFixture(t, coretest.IntegrationConfig())

	projectID, err := f.Orchestrator.StartDeployment(f.OwnerContext(), "demo", "compose.zip", "", bytes.NewReader(coretest.ComposeZip(t, map[string]string{
		"docker-compose.yml": webCompose,
	})))
	require.NoError(t, err)

	project, err := f.Repos.Projects.GetByID(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, f.OwnerID, project.OwnerID)
	require.Equal(t, model.ProjectStatusBuilding, project.Status)

	job, err := f.Repos.Projects.GetActiveComposeDeploymentJobByProjectID(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, model.ComposeDeploymentStatusQueued, job.Status)
	require.NotEmpty(t, job.SourceObjectKey)
	require.True(t, f.Objects.Has(job.SourceObjectKey))
	require.Equal(t, 1, dbtest.CountRows(t, f.Repos.DB, "compose_deployment_queue_outbox"))
	activeBytes, err := f.Repos.Staged.ActiveBytesByOwner(ctx, f.OwnerID)
	require.NoError(t, err)
	require.NotZero(t, activeBytes)

	require.NoError(t, f.Deliver(t))

	job, err = f.Repos.Projects.GetComposeDeploymentJob(ctx, job.ID)
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
	require.False(t, f.Objects.Has(job.SourceObjectKey))
	activeBytes, err = f.Repos.Staged.ActiveBytesByOwner(ctx, f.OwnerID)
	require.NoError(t, err)
	require.Zero(t, activeBytes)
}

func TestComposeUploadAcceptsRequestedComposeFileFromArchive(t *testing.T) {
	files := map[string]string{"deploy/docker-compose.yml": webCompose}
	tests := []struct {
		name        string
		archiveName string
		archive     []byte
		wantSuffix  string
	}{
		{name: "zip", archiveName: "compose.zip", archive: coretest.ComposeZip(t, files), wantSuffix: ".zip"},
		{name: "tar.gz", archiveName: "compose.tar.gz", archive: coretest.ComposeTarGz(t, files), wantSuffix: ".tar.gz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := coretest.NewComposeFixture(t, coretest.IntegrationConfig())

			projectID, err := f.Orchestrator.StartDeployment(f.OwnerContext(), "nested-demo", tt.archiveName, "deploy/docker-compose.yml", bytes.NewReader(tt.archive))
			require.NoError(t, err)

			job, err := f.Repos.Projects.GetActiveComposeDeploymentJobByProjectID(ctx, projectID)
			require.NoError(t, err)
			require.Equal(t, "deploy/docker-compose.yml", job.ComposeFile)
			require.Equal(t, model.ComposeSourceTypeUpload, job.SourceType)
			require.True(t, strings.HasSuffix(job.SourceObjectKey, tt.wantSuffix), job.SourceObjectKey)
			require.True(t, f.Objects.Has(job.SourceObjectKey))
		})
	}
}

func TestComposeUploadWithBuildCreatesProjectBuildJob(t *testing.T) {
	ctx := context.Background()
	f := coretest.NewComposeFixture(t, coretest.IntegrationConfig())

	projectID, err := f.Orchestrator.StartDeployment(f.OwnerContext(), "build-demo", "compose.zip", "", bytes.NewReader(coretest.ComposeZip(t, map[string]string{
		"docker-compose.yml": "services:\n  web:\n    build:\n      context: .\n      dockerfile: Dockerfile\n",
		"Dockerfile":         "FROM scratch\n",
	})))
	require.NoError(t, err)
	job, err := f.Repos.Projects.GetActiveComposeDeploymentJobByProjectID(ctx, projectID)
	require.NoError(t, err)

	require.NoError(t, f.Deliver(t))

	builds, err := f.Repos.Builds.GetByProjectID(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, builds, 1)
	require.Equal(t, projectID, *builds[0].ProjectID)
	require.Equal(t, "web", builds[0].ProjectServiceName)
	require.Equal(t, model.BuildStatusPending, builds[0].Status)

	job, err = f.Repos.Projects.GetComposeDeploymentJob(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, model.ComposeDeploymentStageBuilding, job.Stage)
	require.Contains(t, string(job.ResourceMapJSON), builds[0].ID.String())
	require.False(t, f.Objects.Has(job.SourceObjectKey))
}

func TestComposeCancelBeforeWorkerStartReleasesSource(t *testing.T) {
	ctx := context.Background()
	f := coretest.NewComposeFixture(t, coretest.IntegrationConfig())

	projectID, err := f.Orchestrator.StartDeployment(f.OwnerContext(), "cancel-demo", "compose.zip", "", bytes.NewReader(coretest.ComposeZip(t, map[string]string{
		"docker-compose.yml": webCompose,
	})))
	require.NoError(t, err)
	job, err := f.Repos.Projects.GetActiveComposeDeploymentJobByProjectID(ctx, projectID)
	require.NoError(t, err)
	require.NoError(t, f.Repos.Projects.RequestComposeDeploymentCancel(ctx, projectID))

	require.NoError(t, f.Deliver(t))

	job, err = f.Repos.Projects.GetComposeDeploymentJob(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, model.ComposeDeploymentStatusCanceled, job.Status)
	project, err := f.Repos.Projects.GetByID(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, model.ProjectStatusCanceled, project.Status)
	require.False(t, f.Objects.Has(job.SourceObjectKey))
	activeBytes, err := f.Repos.Staged.ActiveBytesByOwner(ctx, f.OwnerID)
	require.NoError(t, err)
	require.Zero(t, activeBytes)
}

func TestComposeUploadValidationFailuresDoNotCreateDurableJob(t *testing.T) {
	tests := []struct {
		name        string
		cfgEdit     func(*config.SystemConfig)
		archiveName string
		archive     []byte
		composeFile string
		wantErr     error
	}{
		{
			name:    "invalid compose",
			archive: coretest.ComposeZip(t, map[string]string{"docker-compose.yml": "name: empty\n"}),
			wantErr: apperrors.ErrBadRequest,
		},
		{
			name:        "requested compose missing",
			archive:     coretest.ComposeZip(t, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"}),
			composeFile: "deploy/docker-compose.yml",
			wantErr:     apperrors.ErrBadRequest,
		},
		{
			name:        "invalid requested compose path",
			archive:     coretest.ComposeZip(t, map[string]string{"docker-compose.yml": "services:\n  web:\n    image: nginx\n"}),
			composeFile: "../docker-compose.yml",
			wantErr:     apperrors.ErrBadRequest,
		},
		{
			name:        "raw compose with compose_file",
			archiveName: "docker-compose.yml",
			archive:     []byte("services:\n  web:\n    image: nginx\n"),
			composeFile: "deploy/docker-compose.yml",
			wantErr:     apperrors.ErrBadRequest,
		},
		{
			name:        "corrupt tar",
			archiveName: "compose.tar",
			archive:     []byte("not a tar"),
			wantErr:     apperrors.ErrBadRequest,
		},
		{
			name: "disabled image builds with build service",
			cfgEdit: func(c *config.SystemConfig) {
				c.ImageBuildsEnabled = false
			},
			archive: coretest.ComposeZip(t, map[string]string{
				"docker-compose.yml": "services:\n  web:\n    build: .\n",
				"Dockerfile":         "FROM scratch\n",
			}),
			wantErr: apperrors.ErrUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := coretest.IntegrationConfig()
			if tt.cfgEdit != nil {
				tt.cfgEdit(&cfg)
			}
			f := coretest.NewComposeFixture(t, cfg)
			archiveName := tt.archiveName
			if archiveName == "" {
				archiveName = "compose.zip"
			}
			_, err := f.Orchestrator.StartDeployment(f.OwnerContext(), "bad-demo", archiveName, tt.composeFile, bytes.NewReader(tt.archive))
			require.ErrorIs(t, err, tt.wantErr)
			require.Zero(t, dbtest.CountRows(t, f.Repos.DB, "projects"))
			require.Zero(t, dbtest.CountRows(t, f.Repos.DB, "compose_deployment_jobs"))
			activeBytes, err := f.Repos.Staged.ActiveBytesByOwner(context.Background(), f.OwnerID)
			require.NoError(t, err)
			require.Zero(t, activeBytes)
		})
	}
}
