package compose_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// These tests run whole Compose deployment jobs through their entry points:
// the upload request, the deployment message handler, coordinator ticks,
// cancellation and restart cleanup. Workers outside the job (Builder, the
// container and volume lifecycle workers, Docker health) are played by the
// test between ticks, and assertions read job, project and resource state.

// stackCompose is a database with a healthcheck and a managed volume, and a
// built API that waits for the database to be healthy.
const stackCompose = `
services:
  db:
    image: postgres:17-alpine
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
    volumes:
      - data:/var/lib/postgresql/data
  api:
    build:
      context: .
    depends_on:
      db:
        condition: service_healthy
volumes:
  data: {}
`

const restartRecoveryMessage = "deployment interrupted by core service restart"

var errTransientDatabase = apperrors.Wrap(apperrors.ErrUnavailable, "database unavailable", errors.New("connection reset"))

func TestComposeDeploymentJobRunsToDone(t *testing.T) {
	w := newDeploymentWorld(t)
	w.deploy(t, stackCompose)
	require.Equal(t, model.ComposeDeploymentStatusQueued, w.store.job.Status)
	sourceKey := w.store.job.SourceObjectKey

	// Plan: the handler persists the plan and starts the API build.
	require.NoError(t, w.deliver(t))
	w.requireJob(t, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStageBuilding, model.ProjectStatusBuilding)
	require.Len(t, w.builds.builds, 1)
	require.NotContains(t, w.objects.objects, sourceKey, "the build owns its own archive copy")

	// Build: the job waits for Builder, then moves on.
	w.tick(t)
	w.requireJob(t, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStageBuilding, model.ProjectStatusBuilding)
	w.builds.finish(model.BuildStatusSuccess)
	w.tick(t)
	w.requireJob(t, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStageCreating, model.ProjectStatusDeploying)

	// Create: the managed volume and both containers exist.
	w.tick(t)
	w.requireJob(t, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStageStarting, model.ProjectStatusDeploying)
	require.Equal(t, 2, w.containers.count())
	require.Len(t, w.store.serviceGraph, 2)
	require.Contains(t, w.persistedResources(t).VolumeIDs, "data")

	// Wait for dependencies: the database starts first; the API waits until
	// the database is healthy.
	w.tick(t)
	require.Equal(t, model.ContainerStatusStarting, w.service(t, "db").Status)
	require.Equal(t, model.ContainerStatusCreated, w.service(t, "api").Status)
	w.containers.runQueuedStarts()
	w.setHealth(t, "db", "starting")
	w.tick(t)
	require.Equal(t, model.ContainerStatusCreated, w.service(t, "api").Status)

	// Start: the API starts once the database is healthy.
	w.setHealth(t, "db", "healthy")
	w.tick(t)
	require.Equal(t, model.ContainerStatusStarting, w.service(t, "api").Status)
	w.containers.runQueuedStarts()
	w.tick(t)

	w.requireJob(t, model.ComposeDeploymentStatusSucceeded, model.ComposeDeploymentStageStarting, model.ProjectStatusRunning)
	require.Equal(t, model.ContainerStatusRunning, w.service(t, "db").Status)
	require.Equal(t, model.ContainerStatusRunning, w.service(t, "api").Status)
	require.Empty(t, w.volumes.deleted)
	require.Empty(t, w.images.deleted)
	w.requireSourceReleased(t)
}

func TestComposeDeploymentJobRollsBackFailure(t *testing.T) {
	w := newDeploymentWorld(t)
	w.deploy(t, stackCompose)
	require.NoError(t, w.deliver(t))
	w.builds.finish(model.BuildStatusSuccess)
	w.tickUntilStage(t, model.ComposeDeploymentStageStarting)
	w.tick(t)
	w.containers.runQueuedStarts()
	volumeID := w.persistedResources(t).VolumeIDs["data"]

	// The database becomes unhealthy, so the API's dependency fails.
	w.setHealth(t, "db", "unhealthy")
	w.tick(t)

	// Rollback waits for the queued deletes.
	w.requireJob(t, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStageRollingBack, model.ProjectStatusFailed)
	require.Equal(t, model.ContainerStatusDeleting, w.service(t, "db").Status)
	require.Equal(t, model.ContainerStatusDeleting, w.service(t, "api").Status)
	require.Contains(t, w.volumes.deleted, volumeID)

	w.containers.finishDeletes()
	w.volumes.finishDeletes()
	w.tick(t)

	w.requireJob(t, model.ComposeDeploymentStatusFailed, model.ComposeDeploymentStageRollingBack, model.ProjectStatusFailed)
	require.NotNil(t, w.store.job.ErrorMessage)
	require.Contains(t, *w.store.job.ErrorMessage, "unhealthy")
	require.Zero(t, w.containers.count())
	require.True(t, w.volumes.removed[volumeID])
	require.Equal(t, []uuid.UUID{w.onlyBuild(t).ImageID}, w.images.deleted, "the built image is removed with the deployment")
	w.requireSourceReleased(t)
}

func TestComposeDeploymentJobCancellation(t *testing.T) {
	t.Run("while queued", func(t *testing.T) {
		w := newDeploymentWorld(t)
		projectID := w.deploy(t, gitComposeWithoutBuild)
		require.NoError(t, w.orchestrator.CancelDeployment(w.ownerCtx(), projectID))
		require.Equal(t, model.ProjectStatusCanceling, w.store.project.Status)

		require.NoError(t, w.deliver(t))

		w.requireJob(t, model.ComposeDeploymentStatusCanceled, "", model.ProjectStatusCanceled)
		require.Zero(t, w.store.job.Attempts, "a canceled job never runs")
		require.Zero(t, w.containers.count())
		w.requireSourceReleased(t)
	})

	t.Run("while building", func(t *testing.T) {
		w := newDeploymentWorld(t)
		projectID := w.deploy(t, stackCompose)
		require.NoError(t, w.deliver(t))
		require.Equal(t, model.BuildStatusPending, w.onlyBuild(t).Status)
		require.NoError(t, w.orchestrator.CancelDeployment(w.ownerCtx(), projectID))

		w.tick(t)

		w.requireJob(t, model.ComposeDeploymentStatusCanceled, model.ComposeDeploymentStageRollingBack, model.ProjectStatusCanceled)
		require.Equal(t, model.BuildStatusCanceled, w.onlyBuild(t).Status, "the unfinished build is canceled with the deployment")
		require.Empty(t, w.images.deleted, "a canceled build pushed no image to delete")
		require.Zero(t, w.containers.count())
		w.requireSourceReleased(t)
	})

	t.Run("while starting", func(t *testing.T) {
		w := newDeploymentWorld(t)
		projectID := w.deploy(t, gitComposeWithoutBuild)
		require.NoError(t, w.deliver(t))
		w.tickUntilStage(t, model.ComposeDeploymentStageStarting)
		require.NoError(t, w.orchestrator.CancelDeployment(w.ownerCtx(), projectID))

		w.tick(t)
		w.requireJob(t, model.ComposeDeploymentStatusCanceling, model.ComposeDeploymentStageRollingBack, model.ProjectStatusCanceled)
		require.Equal(t, model.ContainerStatusDeleting, w.service(t, "web").Status)

		w.containers.finishDeletes()
		w.tick(t)

		w.requireJob(t, model.ComposeDeploymentStatusCanceled, model.ComposeDeploymentStageRollingBack, model.ProjectStatusCanceled)
		require.Zero(t, w.containers.count())
		w.requireSourceReleased(t)
	})
}

// A errTransientDatabase failure after the job starts but before its plan is durable
// returns the job to queued, so the redelivered message runs ingestion again
// and the job then runs to done.
func TestComposeDeploymentJobRetriesTransientFailureOnRedelivery(t *testing.T) {
	sourceOutage := errors.New("object storage connection reset")
	for _, tc := range []struct {
		name    string
		arrange func(w *deploymentWorld)
		wantErr error
	}{
		{name: "project read fails", arrange: func(w *deploymentWorld) { w.store.faults.GetProject = []error{errTransientDatabase} }, wantErr: errTransientDatabase},
		{name: "source read fails", arrange: func(w *deploymentWorld) { w.objects.readErrs = []error{sourceOutage} }, wantErr: sourceOutage},
		{name: "plan write fails", arrange: func(w *deploymentWorld) { w.store.faults.SavePlan = []error{errTransientDatabase} }, wantErr: errTransientDatabase},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newDeploymentWorld(t)
			w.deploy(t, gitComposeWithoutBuild)
			sourceKey := w.store.job.SourceObjectKey
			tc.arrange(w)

			require.ErrorIs(t, w.deliver(t), tc.wantErr, "the message stays retryable")
			w.requireJob(t, model.ComposeDeploymentStatusQueued, "", model.ProjectStatusBuilding)
			require.Contains(t, w.objects.objects, sourceKey, "the source survives for the retry")

			require.NoError(t, w.deliver(t))
			w.requireJob(t, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStageCreating, model.ProjectStatusDeploying)
			require.Equal(t, 2, w.store.job.Attempts)

			w.tickUntilStage(t, model.ComposeDeploymentStageStarting)
			w.tick(t)
			w.containers.runQueuedStarts()
			w.tick(t)

			w.requireJob(t, model.ComposeDeploymentStatusSucceeded, model.ComposeDeploymentStageStarting, model.ProjectStatusRunning)
			require.Equal(t, model.ContainerStatusRunning, w.service(t, "web").Status)
			w.requireSourceReleased(t)
		})
	}
}

func TestComposeDeploymentJobInterruptedCleanup(t *testing.T) {

	// strandUnplannedJob leaves the job running without a plan: the plan write
	// and the requeue both fail, so only restart recovery can settle it.
	strandUnplannedJob := func(t *testing.T, w *deploymentWorld) {
		t.Helper()
		w.deploy(t, gitComposeWithoutBuild)
		w.store.faults.SavePlan = []error{errTransientDatabase}
		w.store.faults.RequeueJob = []error{errors.New("requeue failed")}
		require.ErrorIs(t, w.deliver(t), errTransientDatabase)
		require.NoError(t, w.deliver(t), "a redelivery acknowledges a job that is not queued")
		w.requireJob(t, model.ComposeDeploymentStatusRunning, "", model.ProjectStatusBuilding)
	}

	t.Run("unplanned job with leftover resources fails and is cleaned", func(t *testing.T) {
		w := newDeploymentWorld(t)
		strandUnplannedJob(t, w)
		projectID := w.store.project.ID
		containerID, err := w.containers.Create(w.ownerCtx(), model.ContainerCreateParams{ProjectID: &projectID, Name: "wh_web"})
		require.NoError(t, err)
		w.resources.containers = []model.Container{w.service(t, "web")}
		volumeID := uuid.New()
		w.resources.volumes = []model.Volume{{ID: volumeID}}
		buildID, _, err := w.builds.CreateProjectBuildJob(w.ownerCtx(), projectID, "web", "", "", "", "", "", nil, "")
		require.NoError(t, err)

		require.NoError(t, w.orchestrator.CleanupInterruptedDeployments(context.Background(), restartRecoveryMessage))

		w.requireJob(t, model.ComposeDeploymentStatusFailed, "", model.ProjectStatusFailed)
		require.Equal(t, restartRecoveryMessage, *w.store.job.ErrorMessage)
		require.Equal(t, model.ContainerStatusDeleting, w.containers.containers[containerID].Status)
		w.volumes.finishDeletes()
		require.True(t, w.volumes.removed[volumeID])
		require.Equal(t, model.BuildStatusCanceled, w.builds.builds[buildID].Status)
		w.requireSourceReleased(t)
	})

	t.Run("unplanned job without resources is left to repository recovery", func(t *testing.T) {
		w := newDeploymentWorld(t)
		strandUnplannedJob(t, w)

		require.NoError(t, w.orchestrator.CleanupInterruptedDeployments(context.Background(), restartRecoveryMessage))

		w.requireJob(t, model.ComposeDeploymentStatusRunning, "", model.ProjectStatusBuilding)
		require.Contains(t, w.objects.objects, w.store.job.SourceObjectKey)
	})

	t.Run("planned job is resumed by the coordinator", func(t *testing.T) {
		w := newDeploymentWorld(t)
		w.deploy(t, gitComposeWithoutBuild)
		require.NoError(t, w.deliver(t))

		require.NoError(t, w.orchestrator.CleanupInterruptedDeployments(context.Background(), restartRecoveryMessage))
		w.requireJob(t, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStageCreating, model.ProjectStatusDeploying)

		w.tick(t)
		w.requireJob(t, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStageStarting, model.ProjectStatusDeploying)
	})
}

// deploymentWorld wires the orchestrator and the coordinator the way Core's
// app does, over the stateful Compose fakes.
type deploymentWorld struct {
	ownerID      uuid.UUID
	store        *deploymentRepo
	objects      *composeObjects
	builds       *coordinatorBuilds
	containers   *coordinatorContainers
	volumes      *coordinatorVolumes
	resources    *coordinatorResources
	docker       *coordinatorDocker
	images       *coordinatorImages
	orchestrator *compose.Orchestrator
	coordinator  *compose.DeploymentCoordinator
}

func newDeploymentWorld(t *testing.T) *deploymentWorld {
	t.Helper()
	w := &deploymentWorld{
		ownerID:    uuid.New(),
		store:      &deploymentRepo{},
		objects:    &composeObjects{objects: make(map[string][]byte)},
		builds:     &coordinatorBuilds{},
		containers: newCoordinatorContainers(nil),
		volumes:    &coordinatorVolumes{},
		resources:  &coordinatorResources{},
		docker:     &coordinatorDocker{inspections: make(map[string]model.ContainerInspection)},
		images:     &coordinatorImages{},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := staticConfig{cfg: config.SystemConfig{
		ImageBuildsEnabled:                true,
		ComposeUploadMaxBytes:             1024 * 1024,
		ComposeCoordinatorIntervalSeconds: 1,
	}}
	orchestrator, err := compose.NewOrchestrator(compose.OrchestratorDeps{
		Projects:          w.store,
		DeploymentCounter: w.store,
		Capacity:          admitAllCapacity{},
		Config:            cfg,
		Builds:            w.builds,
		Resources:         w.resources,
		Volumes:           w.volumes,
		Containers:        w.containers,
		ObjectStore:       w.objects,
		BuildJobs:         w.builds,
		Images:            w.images,
		Logger:            logger,
	})
	require.NoError(t, err)
	coordinator, err := compose.NewDeploymentCoordinator(compose.DeploymentCoordinatorDeps{
		Deployments:       w.store,
		Builds:            w.builds,
		Builder:           compose.NewLocalBuilderClient(w.objects, w.builds),
		Containers:        w.containers,
		ProjectContainers: w.resources,
		Volumes:           w.volumes,
		Docker:            w.docker,
		Images:            w.images,
		Sources:           orchestrator,
		Config:            cfg,
		Logger:            logger,
	})
	require.NoError(t, err)
	w.orchestrator = orchestrator
	w.coordinator = coordinator
	return w
}

func (w *deploymentWorld) ownerCtx() context.Context {
	return accessscope.WithUserScope(context.Background(), w.ownerID, "", "")
}

// deploy uploads composeYAML as project "wh" and returns the project ID.
func (w *deploymentWorld) deploy(t *testing.T, composeYAML string) uuid.UUID {
	t.Helper()
	projectID, err := w.orchestrator.StartDeployment(w.ownerCtx(), "wh", "source.zip", "", bytes.NewReader(zipComposeArchive(t, composeYAML)))
	require.NoError(t, err)
	return projectID
}

// deliver hands the job's queued outbox message to the message handler.
func (w *deploymentWorld) deliver(t *testing.T) error {
	t.Helper()
	require.NotEmpty(t, w.store.outbox)
	var msg queue.DeploymentMessage
	require.NoError(t, json.Unmarshal(w.store.outbox[len(w.store.outbox)-1].Payload, &msg))
	return w.orchestrator.HandleDeploymentMessage(context.Background(), msg)
}

// tick runs one coordinator pass over the active jobs.
func (w *deploymentWorld) tick(t *testing.T) {
	t.Helper()
	runCoordinatorTick(w.coordinator, w.store)
}

func (w *deploymentWorld) tickUntilStage(t *testing.T, stage string) {
	t.Helper()
	for range 5 {
		if w.store.job.Stage == stage {
			return
		}
		w.tick(t)
	}
	require.Equal(t, stage, w.store.job.Stage)
}

func (w *deploymentWorld) requireJob(t *testing.T, status, stage, projectStatus string) {
	t.Helper()
	require.Equal(t, status, w.store.job.Status, "job status")
	require.Equal(t, stage, w.store.job.Stage, "job stage")
	require.Equal(t, projectStatus, w.store.project.Status, "project status")
}

// requireSourceReleased checks that the job's staged Compose source is gone.
// Build archive copies belong to their builds, so they are not checked.
func (w *deploymentWorld) requireSourceReleased(t *testing.T) {
	t.Helper()
	require.NotContains(t, w.objects.objects, w.store.job.SourceObjectKey)
}

func (w *deploymentWorld) persistedResources(t *testing.T) persistedResources {
	t.Helper()
	var resources persistedResources
	require.NoError(t, json.Unmarshal(w.store.job.ResourceMapJSON, &resources))
	return resources
}

// service returns the container of a Compose service in project "wh".
func (w *deploymentWorld) service(t *testing.T, name string) model.Container {
	t.Helper()
	return w.containers.byName(t, "wh_"+name)
}

func (w *deploymentWorld) onlyBuild(t *testing.T) model.Build {
	t.Helper()
	require.Len(t, w.builds.builds, 1)
	for _, build := range w.builds.builds {
		return build
	}
	return model.Build{}
}

// setHealth plays Docker's healthcheck for a running service.
func (w *deploymentWorld) setHealth(t *testing.T, service, health string) {
	t.Helper()
	container := w.service(t, service)
	require.NotEmpty(t, container.DockerID, "service %s has not started", service)
	w.docker.inspections[container.DockerID] = model.ContainerInspection{
		Healthcheck: &model.Healthcheck{Test: []string{"CMD-SHELL", "true"}},
		State:       model.ContainerState{Status: "running", Running: true, HealthStatus: &health},
	}
}

// composeObjects is in-memory object storage. Each queued read error fails
// one source read.
type composeObjects struct {
	objects  map[string][]byte
	readErrs []error
}

func (s *composeObjects) UploadStream(_ context.Context, objectKey string, reader io.Reader, _ int64, _ string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	s.objects[objectKey] = data
	return nil
}

func (s *composeObjects) DeleteObject(_ context.Context, objectKey string) error {
	delete(s.objects, objectKey)
	return nil
}

func (s *composeObjects) CopyObject(_ context.Context, sourceKey, destKey, _ string) error {
	data, ok := s.objects[sourceKey]
	if !ok {
		return apperrors.ErrNotFound
	}
	s.objects[destKey] = bytes.Clone(data)
	return nil
}

func (s *composeObjects) OpenObject(_ context.Context, objectKey string) (io.ReadCloser, error) {
	data, ok := s.objects[objectKey]
	if !ok {
		return nil, apperrors.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *composeObjects) NewReaderAt(_ context.Context, objectKey string) (io.ReaderAt, int64, error) {
	if err := popErr(&s.readErrs); err != nil {
		return nil, 0, err
	}
	data, ok := s.objects[objectKey]
	if !ok {
		return nil, 0, apperrors.ErrNotFound
	}
	return bytes.NewReader(data), int64(len(data)), nil
}

type admitAllCapacity struct{}

func (admitAllCapacity) CheckCapacity(context.Context, uuid.UUID, int64, int64, int64) error {
	return nil
}
