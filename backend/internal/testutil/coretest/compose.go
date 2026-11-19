package coretest

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/compose"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ComposeFixture wires Core's Compose deployment path as Core's app does:
// real repositories over an isolated Postgres schema, and the real build,
// container, volume and image services and Compose orchestrator and
// coordinator. Object storage is in memory. The Docker mocks start with no
// expectations, so a test states every Docker call it allows. The Registry
// mock answers image metadata reads; host metrics and the owner's SSO record
// are generous stubs.
type ComposeFixture struct {
	Repos   CoreRepositories
	Config  StaticConfig
	OwnerID uuid.UUID
	Objects *MemoryObjects

	Builds       *service.BuildService
	Containers   *service.ContainerService
	Volumes      *service.VolumeService
	Images       *service.ImageService
	Orchestrator *compose.Orchestrator
	Coordinator  *compose.DeploymentCoordinator

	ContainerDocker *coremocks.ContainerDockerAPI
	VolumeDocker    *coremocks.VolumeDockerAPI
	ImageDocker     *coremocks.ImageDockerAPI
	Registry        *coremocks.ImageRegistryAPI

	deployments *tickingDeploymentStore
}

// NewComposeFixture wires the Compose deployment path with cfg. The
// coordinator interval is set to its one-second minimum, so each Tick takes
// about a second.
func NewComposeFixture(t *testing.T, cfg config.SystemConfig) *ComposeFixture {
	t.Helper()
	cfg.ComposeCoordinatorIntervalSeconds = 1
	f := &ComposeFixture{
		Repos:           OpenCoreRepositories(t),
		Config:          StaticConfig{Config: cfg},
		OwnerID:         uuid.New(),
		Objects:         NewMemoryObjects(),
		ContainerDocker: coremocks.NewContainerDockerAPI(t),
		VolumeDocker:    coremocks.NewVolumeDockerAPI(t),
		ImageDocker:     coremocks.NewImageDockerAPI(t),
		Registry:        coremocks.NewImageRegistryAPI(t),
	}
	repos := f.Repos
	logger := DiscardLogger()

	// The coordinator reads the owner's username for audit events only.
	users := coremocks.NewUserInfoProvider(t)
	users.EXPECT().GetUser(mock.Anything, f.OwnerID).
		Return(model.UserInfo{ID: f.OwnerID, Username: "composer"}, nil).Maybe()
	repos.SetCustomQuota(t, f.OwnerID, model.UserQuota{CPUMillicores: 4000, RAMMB: 4096, DiskMB: 4096})
	quotas := repos.UserQuotas(t, f.Config)
	hostMetrics := coremocks.NewHostMetricsProvider(t)
	hostMetrics.EXPECT().GetTotalMemory().Return(int64(1<<40), nil).Maybe()
	hostMetrics.EXPECT().GetLogicalCPUs().Return(int64(64), nil).Maybe()
	diskMetrics := coremocks.NewHostDiskMetricsProvider(t)
	diskMetrics.EXPECT().GetDiskUsage(mock.Anything).Return(model.HostDiskStats{TotalBytes: 1 << 41, FreeBytes: 1 << 40}, nil).Maybe()
	f.Registry.EXPECT().GetImageSizeAndDigest(mock.Anything, mock.Anything, mock.Anything).Return(int64(1<<20), "sha256:digest", nil).Maybe()

	imageCache := coremocks.NewBuildImageCache(t)
	imageCache.EXPECT().RemoveImage(mock.Anything, mock.Anything, false).Return(nil).Maybe()

	var err error
	f.Builds, err = service.NewBuildService(service.BuildServiceDeps{
		Repository:     repos.Builds,
		RegistryAuth:   repos.RegistryAuthorization(t),
		Images:         repos.Images,
		Registry:       f.Registry,
		Docker:         imageCache,
		Quotas:         quotas,
		Logger:         logger,
		Containers:     repos.Containers,
		CapacityLock:   repos.Containers,
		HostMetrics:    hostMetrics,
		VolumeRepo:     repos.Volumes,
		Config:         f.Config,
		ObjectStore:    f.Objects,
		StagedObjects:  repos.Staged,
		DiskMetrics:    diskMetrics,
		DockerDataPath: "/",
	})
	require.NoError(t, err)
	f.Containers, err = service.NewContainerService(service.ContainerServiceDeps{
		Repository:     repos.Containers,
		Volumes:        repos.Volumes,
		Images:         repos.Images,
		Docker:         f.ContainerDocker,
		Metrics:        hostMetrics,
		DiskMetrics:    diskMetrics,
		Config:         f.Config,
		Quotas:         quotas,
		Builds:         repos.Builds,
		DockerDataPath: "/",
		Logger:         logger,
	})
	require.NoError(t, err)
	f.Volumes, err = service.NewVolumeService(service.VolumeServiceDeps{
		Repository:     repos.Volumes,
		Docker:         f.VolumeDocker,
		Config:         f.Config,
		Lifecycle:      repos.Lifecycle,
		Quotas:         quotas,
		ImageRepo:      repos.Images,
		DiskMetrics:    diskMetrics,
		CapacityLock:   repos.Containers,
		SizeMode:       MeasuredVolumeSizeMode(t),
		DockerDataPath: "/",
	})
	require.NoError(t, err)
	f.Images, err = service.NewImageService(service.ImageServiceDeps{
		Repository:   repos.Images,
		Docker:       f.ImageDocker,
		Registry:     f.Registry,
		Containers:   repos.Containers,
		Config:       f.Config,
		Lifecycle:    repos.Lifecycle,
		RegistryAuth: repos.RegistryAuthorization(t),
	})
	require.NoError(t, err)

	planAdmission, err := service.NewComposePlanAdmission(service.ComposePlanAdmissionDeps{
		Containers:     repos.Containers,
		Volumes:        repos.Volumes,
		Builds:         repos.Builds,
		Images:         repos.Images,
		Deployments:    repos.Projects,
		Quotas:         quotas,
		HostMetrics:    hostMetrics,
		DiskMetrics:    diskMetrics,
		DockerDataPath: "/",
		Config:         f.Config,
		Logger:         logger,
	})
	require.NoError(t, err)

	resources := ProjectResources{Containers: repos.Containers, Volumes: repos.Volumes}
	f.Orchestrator, err = compose.NewOrchestrator(compose.OrchestratorDeps{
		Projects:       repos.Projects,
		PlanAdmission:  planAdmission,
		Config:         f.Config,
		Builds:         repos.Builds,
		Resources:      resources,
		Volumes:        f.Volumes,
		Containers:     f.Containers,
		ObjectStore:    f.Objects,
		BuildJobs:      f.Builds,
		Images:         f.Images,
		StagedObjects:  repos.Staged,
		CapacityLock:   repos.Containers,
		ReservedDisk:   service.NewReservedDiskReader(repos.Images, repos.Volumes),
		DiskMetrics:    diskMetrics,
		DockerDataPath: "/",
		Quotas:         quotas,
		Logger:         logger,
	})
	require.NoError(t, err)
	f.Builds.SetDeploymentCanceler(f.Orchestrator)

	f.deployments = &tickingDeploymentStore{ProjectRepository: repos.Projects}
	f.Coordinator, err = compose.NewDeploymentCoordinator(compose.DeploymentCoordinatorDeps{
		Deployments:       f.deployments,
		Builds:            repos.Builds,
		Builder:           compose.NewLocalBuilderClient(f.Objects, f.Builds),
		Containers:        f.Containers,
		ProjectContainers: resources,
		Volumes:           f.Volumes,
		Docker:            f.ContainerDocker,
		Images:            f.Images,
		Sources:           f.Orchestrator,
		Config:            f.Config,
		Users:             users,
		Logger:            logger,
	})
	require.NoError(t, err)
	return f
}

// OwnerContext is the deploying owner's user scope.
func (f *ComposeFixture) OwnerContext() context.Context {
	return UserContext(f.OwnerID)
}

// Deliver plays the Compose outbox relay and broker: it leases the pending
// deployment messages and hands each to the deployment message handler,
// returning the first handler error. It fails t when nothing is pending.
func (f *ComposeFixture) Deliver(t *testing.T) error {
	t.Helper()
	ctx := context.Background()
	items, err := f.Repos.Projects.LeasePendingComposeOutbox(ctx, 10)
	require.NoError(t, err)
	require.NotEmpty(t, items, "no Compose deployment message is pending")
	var first error
	for _, item := range items {
		require.NoError(t, f.Repos.Projects.MarkComposeOutboxPublished(ctx, item.ID))
		var msg queue.DeploymentMessage
		require.NoError(t, json.Unmarshal(item.Payload, &msg))
		if err := f.Orchestrator.HandleDeploymentMessage(ctx, msg); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Tick runs one coordinator pass over the active deployment jobs.
func (f *ComposeFixture) Tick(t *testing.T) {
	t.Helper()
	// The timeout keeps a coordinator that stopped listing once per tick
	// from hanging the test; the deadline failing t names that cause.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f.deployments.startPass(cancel)
	f.Coordinator.Run(ctx)
	require.NotErrorIs(t, ctx.Err(), context.DeadlineExceeded, "the coordinator did not finish one pass")
}

// RunContainerOperations plays the container outbox relay, broker and
// container lifecycle worker for every pending container operation, and
// returns how many ran.
func (f *ComposeFixture) RunContainerOperations(t *testing.T) int {
	t.Helper()
	ctx := context.Background()
	items, err := f.Repos.Containers.LeasePendingContainerOutbox(ctx, 50)
	require.NoError(t, err)
	worker := service.NewContainerCreateWorker(f.Containers, f.Config, DiscardLogger())
	for _, item := range items {
		require.NoError(t, f.Repos.Containers.MarkContainerOutboxPublished(ctx, item.ID))
		var msg queue.ContainerLifecycleMessage
		require.NoError(t, json.Unmarshal(item.Payload, &msg))
		require.NoError(t, worker.HandleMessage(ctx, msg))
	}
	return len(items)
}

// RunResourceOperations plays the resource outbox relay, broker and resource
// lifecycle worker for every pending volume and image operation, and returns
// how many ran.
func (f *ComposeFixture) RunResourceOperations(t *testing.T) int {
	t.Helper()
	ctx := context.Background()
	items, err := f.Repos.Lifecycle.LeasePendingResourceOutbox(ctx, 50)
	require.NoError(t, err)
	worker := service.NewResourceLifecycleWorker(f.Volumes, f.Images, f.Config, DiscardLogger())
	for _, item := range items {
		require.NoError(t, f.Repos.Lifecycle.MarkResourceOutboxPublished(ctx, item.ID))
		var msg queue.ResourceLifecycleMessage
		require.NoError(t, json.Unmarshal(item.Payload, &msg))
		require.NoError(t, worker.HandleMessage(ctx, msg))
	}
	return len(items)
}

// RecoverOperations runs one pass of the operation recovery worker over the
// fixture's repositories, as Core does every minute.
func (f *ComposeFixture) RecoverOperations(t *testing.T) {
	t.Helper()
	worker, err := service.NewOperationRecoveryWorker(service.OperationRecoveryWorkerDeps{
		Containers:  f.Repos.Containers,
		Resources:   f.Repos.Lifecycle,
		Deployments: f.Repos.Projects,
		Config:      f.Config,
		Logger:      DiscardLogger(),
	})
	require.NoError(t, err)
	worker.RunPass(context.Background())
}

// tickingDeploymentStore is the production deployment store, except that
// the second job listing of a pass stops the coordinator run. The coordinator
// lists once per tick, so a run advances the active jobs exactly once with
// an uncanceled context.
type tickingDeploymentStore struct {
	*repository.ProjectRepository

	mu     sync.Mutex
	listed int
	stop   context.CancelFunc
}

func (s *tickingDeploymentStore) startPass(stop context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listed = 0
	s.stop = stop
}

func (s *tickingDeploymentStore) ListActiveComposeDeploymentJobs(ctx context.Context, limit int) ([]model.ComposeDeploymentJob, error) {
	s.mu.Lock()
	s.listed++
	again := s.listed > 1
	s.mu.Unlock()
	if again {
		s.stop()
		return nil, nil
	}
	return s.ProjectRepository.ListActiveComposeDeploymentJobs(ctx, limit)
}

// ProjectResources is the Compose view of a project's containers and
// volumes, as Core's app adapts its repositories.
type ProjectResources struct {
	Containers *repository.ContainerRepository
	Volumes    *repository.VolumeRepository
}

func (r ProjectResources) GetByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Container, error) {
	return r.Containers.GetByProjectID(ctx, projectID)
}

func (r ProjectResources) GetVolumesByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Volume, error) {
	return r.Volumes.GetByProjectID(ctx, projectID)
}

// ComposeZip builds a ZIP Compose source archive from file names and contents.
func ComposeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, contents := range files {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = io.WriteString(w, contents)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// ComposeTarGz builds a gzip-compressed tar Compose source archive from file
// names and contents.
func ComposeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	for name, contents := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(contents))}))
		_, err := io.WriteString(tw, contents)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gzw.Close())
	return buf.Bytes()
}

// MeasuredVolumeSizeMode reports every volume as created in measured mode, as
// with volume_size_enforcement off.
func MeasuredVolumeSizeMode(t *testing.T) *coremocks.VolumeSizeModeReader {
	t.Helper()
	mode := coremocks.NewVolumeSizeModeReader(t)
	mode.EXPECT().VolumeSizeMode().Return(model.VolumeSizeMeasured).Maybe()
	return mode
}
