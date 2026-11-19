package service_test

import (
	"context"
	"log/slog"
	"testing"
	"testing/synctest"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/auditlog"
	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newContainerRepository(t *testing.T) *coremocks.ContainerRepository {
	t.Helper()
	repo := coremocks.NewContainerRepository(t)
	repo.EXPECT().AcquireOwnerCapacityLock(mock.Anything, mock.Anything).Return(func() {}, nil).Maybe()
	repo.EXPECT().CountActiveCreateOperationsByOwner(mock.Anything, mock.Anything).Return(0, nil).Maybe()
	repo.EXPECT().HasActiveOperation(mock.Anything, mock.Anything, mock.Anything).Return(false, nil).Maybe()
	return repo
}

func newContainerImageRepository(t *testing.T) *coremocks.ContainerImageRepository {
	t.Helper()
	repo := coremocks.NewContainerImageRepository(t)
	repo.EXPECT().GetUserImageBytes(mock.Anything, mock.Anything).Return(int64(0), nil).Maybe()
	return repo
}

func newTestContainerService(t *testing.T, deps service.ContainerServiceDeps) *service.ContainerService {
	t.Helper()
	if deps.Repository == nil {
		deps.Repository = newContainerRepository(t)
	}
	if deps.Volumes == nil {
		volumes := coremocks.NewContainerVolumeRepository(t)
		volumes.EXPECT().GetUserReservedVolumeBytes(mock.Anything, mock.Anything).Return(int64(0), nil).Maybe()
		deps.Volumes = volumes
	}
	if deps.Images == nil {
		deps.Images = newContainerImageRepository(t)
	}
	if deps.Docker == nil {
		deps.Docker = coremocks.NewContainerDockerAPI(t)
	}
	if deps.Metrics == nil {
		deps.Metrics = coremocks.NewHostMetricsProvider(t)
	}
	if deps.DiskMetrics == nil {
		deps.DiskMetrics = coremocks.NewHostDiskMetricsProvider(t)
	}
	if deps.Config == nil {
		deps.Config = testConfig()
	}
	if deps.Quotas == nil {
		deps.Quotas = testQuotas(t, deps.Config, nil)
	}
	if deps.Builds == nil {
		builds := coremocks.NewActiveSystemBuildCounter(t)
		builds.EXPECT().CountActive(mock.Anything).Return(0, nil).Maybe()
		deps.Builds = builds
	}
	svc, err := service.NewContainerService(deps)
	require.NoError(t, err)
	return svc
}

func validContainerServiceDeps(t *testing.T) service.ContainerServiceDeps {
	t.Helper()
	return service.ContainerServiceDeps{
		Repository:  coremocks.NewContainerRepository(t),
		Volumes:     coremocks.NewContainerVolumeRepository(t),
		Images:      coremocks.NewContainerImageRepository(t),
		Docker:      coremocks.NewContainerDockerAPI(t),
		Metrics:     coremocks.NewHostMetricsProvider(t),
		DiskMetrics: coremocks.NewHostDiskMetricsProvider(t),
		Config:      testConfig(),
		Quotas:      testQuotas(t, nil, nil),
		Builds:      coremocks.NewActiveSystemBuildCounter(t),
	}
}

func TestNewContainerServiceRequiresDependencies(t *testing.T) {
	depstest.RequireRejectsMissing(t, validContainerServiceDeps, service.NewContainerService, map[string]string{
		"Repository":  "repository",
		"Volumes":     "volumes",
		"Images":      "images",
		"Docker":      "docker",
		"Metrics":     "metrics",
		"DiskMetrics": "disk metrics",
		"Config":      "config",
		"Quotas":      "quotas",
		"Builds":      "builds",
	})
}

func TestContainerServiceAdmissionPropagatesLockFailure(t *testing.T) {
	for _, operation := range []string{"create", "start", "check capacity"} {
		t.Run(operation, func(t *testing.T) {
			owner, id := uuid.New(), uuid.New()
			deps := validContainerServiceDeps(t)
			repo := deps.Repository.(*coremocks.ContainerRepository)
			repo.EXPECT().AcquireOwnerCapacityLock(mock.Anything, owner).Return(nil, apperrors.ErrUnavailable).Once()
			if operation == "start" {
				repo.EXPECT().GetByID(mock.Anything, id).Return(model.Container{ID: id, OwnerID: owner, DockerID: "runtime"}, nil)
			}
			deps.Config = testConfig()
			svc, err := service.NewContainerService(deps)
			require.NoError(t, err)
			ctx := accessscope.WithUserScope(context.Background(), owner, "", "")
			switch operation {
			case "create":
				_, err = svc.Create(ctx, model.ContainerCreateParams{Name: "web", ImageTag: "nginx"})
			case "start":
				err = svc.Start(ctx, id)
			case "check capacity":
				err = svc.CheckCapacity(ctx, owner, 128, 250)
			}
			require.ErrorIs(t, err, apperrors.ErrUnavailable)
		})
	}
}

func TestContainerServiceCreateEnforcesQueuedCreateLimit(t *testing.T) {
	owner := uuid.New()
	deps := validContainerServiceDeps(t)
	repo := deps.Repository.(*coremocks.ContainerRepository)
	released := false
	repo.EXPECT().AcquireOwnerCapacityLock(mock.Anything, owner).Return(func() { released = true }, nil).Once()
	repo.EXPECT().CountByOwnerID(mock.Anything, owner).Return(1, nil).Once()
	repo.EXPECT().CountActiveCreateOperationsByOwner(mock.Anything, owner).Return(2, nil).Once()
	cfg := containerCreateConfig()
	cfg.MaxQueuedContainerCreatesPerUser = 2
	configProvider := coretest.StaticConfig{Config: cfg}
	deps.Config = configProvider
	svc, err := service.NewContainerService(deps)
	require.NoError(t, err)
	_, err = svc.Create(accessscope.WithUserScope(context.Background(), owner, "", ""), model.ContainerCreateParams{Name: "web", ImageTag: "nginx"})
	require.ErrorIs(t, err, apperrors.ErrLimitExceeded)
	require.Equal(t, apperrors.LimitQueuedContainerCreates, apperrors.LimitOf(err))
	require.True(t, released)
}

// Container create refuses a mount of a volume that is not available, before
// it records anything: a missing volume keeps its unavailable error and any
// other status, such as a volume being deleted, is a conflict.
func TestContainerCreateMountsOnlyAvailableVolumes(t *testing.T) {
	for _, tc := range []struct {
		status      string
		wantMessage string
	}{
		{status: model.VolumeStatusCreating, wantMessage: "volume is not available"},
		{status: model.VolumeStatusDeleting, wantMessage: "volume is not available"},
		{status: model.VolumeStatusError, wantMessage: "volume is not available"},
		{status: model.VolumeStatusMissing, wantMessage: "volume is missing in Docker and can only be deleted"},
	} {
		t.Run(tc.status, func(t *testing.T) {
			ownerID, volumeID := uuid.New(), uuid.New()
			w := roomyAdmissionWorld()
			cfg := coretest.StaticConfig{Config: w.config()}
			store := newContainerStore()
			volumes := coremocks.NewContainerVolumeRepository(t)
			volumes.EXPECT().GetUserReservedVolumeBytes(mock.Anything, ownerID).Return(int64(0), nil).Maybe()
			volumes.EXPECT().GetByID(mock.Anything, volumeID).Return(model.Volume{ID: volumeID, OwnerID: ownerID, Name: "data", Status: tc.status}, nil)
			images := newContainerImageRepository(t)
			images.EXPECT().FindByOwnerAndTag(mock.Anything, mock.Anything, mock.Anything).Return(model.Image{}, false, nil).Maybe()
			svc := newTestContainerService(t, service.ContainerServiceDeps{
				Repository:     store,
				Volumes:        volumes,
				Images:         images,
				Metrics:        admissionHostMetrics(t),
				DiskMetrics:    admissionDiskMetrics(t, w),
				DockerDataPath: admissionDockerDataPath,
				Config:         cfg,
				Quotas:         w.quotas(t, ownerID, cfg),
				Logger:         slog.New(slog.DiscardHandler),
			})

			id, err := svc.Create(accessscope.WithUserScope(context.Background(), ownerID, "", ""), model.ContainerCreateParams{
				Name: "web", ImageTag: "nginx:latest",
				VolumeMounts: []model.VolumeMountParams{{VolumeID: volumeID, MountPath: "/data"}},
			})

			require.ErrorIs(t, err, apperrors.ErrConflict)
			require.Equal(t, tc.wantMessage, apperrors.SafeMessage(err))
			require.Equal(t, uuid.Nil, id)
			count, countErr := store.CountByOwnerID(context.Background(), ownerID)
			require.NoError(t, countErr)
			require.Zero(t, count, "no container is recorded")
			requireNothingQueued(t, store)
		})
	}
}

// runtimeRequest is a request that needs the container's Docker runtime.
type runtimeRequest struct {
	name string
	run  func(ctx context.Context, svc *service.ContainerService, id uuid.UUID) error
}

var runtimeRequests = []runtimeRequest{
	{name: "start", run: func(ctx context.Context, svc *service.ContainerService, id uuid.UUID) error {
		return svc.Start(ctx, id)
	}},
	{name: "stop", run: func(ctx context.Context, svc *service.ContainerService, id uuid.UUID) error { return svc.Stop(ctx, id) }},
	{name: "expose", run: func(ctx context.Context, svc *service.ContainerService, id uuid.UUID) error {
		return svc.Expose(ctx, id, "app", 8080)
	}},
}

// Start, stop and expose sent while the container is still being created (no
// Docker container recorded yet, and pending, creating or with its create
// still queued) answer a conflict and write nothing. Only a container whose
// runtime is gone outside a create is marked missing. A refused expose is
// still audited.
func TestRuntimeRequestsWaitForTheContainerCreate(t *testing.T) {
	for _, state := range []struct {
		name         string
		status       string
		activeCreate bool
		wantMissing  bool
	}{
		{name: "pending", status: model.ContainerStatusPending},
		{name: "creating", status: model.ContainerStatusCreating},
		{name: "create still queued", status: model.ContainerStatusCreated, activeCreate: true},
		{name: "runtime lost", status: model.ContainerStatusExited, wantMissing: true},
	} {
		for _, request := range runtimeRequests {
			t.Run(state.name+"/"+request.name, func(t *testing.T) {
				ctx := context.Background()
				ownerID := uuid.New()
				store := newContainerStore()
				c := model.Container{
					ID: uuid.New(), OwnerID: ownerID, Name: "web", ImageTag: "nginx:latest",
					Status: state.status, DesiredStatus: model.ContainerStatusCreated, DockerGeneration: 1,
				}
				createOp := model.ResourceOperation{
					ID: uuid.New(), ResourceType: model.ResourceTypeContainer, ResourceID: c.ID, OwnerID: ownerID,
					Operation: model.OperationCreate, Status: model.OperationStatusPending,
				}
				if state.activeCreate {
					outbox := model.ContainerLifecycleOutbox{ID: uuid.New(), OperationID: createOp.ID, ContainerID: c.ID}
					require.NoError(t, store.SaveWithMountsOperationAndOutbox(ctx, c, nil, createOp, outbox, false, false))
				} else {
					saveContainer(t, store, c)
				}
				svc := newTestContainerService(t, service.ContainerServiceDeps{Repository: store, Logger: slog.New(slog.DiscardHandler)})
				auditor := &recordingAuditRecorder{}
				svc.SetAuditRecorder(auditor)

				err := request.run(accessscope.WithUserScope(ctx, ownerID, "", ""), svc, c.ID)

				require.ErrorIs(t, err, apperrors.ErrConflict)
				got, getErr := store.GetByID(ctx, c.ID)
				require.NoError(t, getErr)
				require.Equal(t, model.ContainerStatusCreated, got.DesiredStatus)
				if state.wantMissing {
					require.Equal(t, "container is missing in Docker and can only be deleted", apperrors.SafeMessage(err))
					require.Equal(t, model.ContainerStatusMissing, got.Status)
				} else {
					require.Equal(t, "container is still being created", apperrors.SafeMessage(err))
					require.Equal(t, state.status, got.Status, "the status is not written")
					require.Nil(t, got.LastError)
				}
				outbox, leaseErr := store.LeasePendingContainerOutbox(ctx, 10)
				require.NoError(t, leaseErr)
				if state.activeCreate {
					require.Len(t, outbox, 1)
					require.Equal(t, createOp.ID, outbox[0].OperationID, "only the create is queued")
				} else {
					require.Empty(t, outbox, "no operation is queued")
				}
				if request.name == "expose" {
					require.Len(t, auditor.events, 1, "the refused expose is audited")
					require.Equal(t, auditlog.OutcomeFailure, auditor.events[0].Outcome)
				}
			})
		}
	}
}

// A start sent too early is refused without harm: the queued create then
// completes and the container starts.
func TestAContainerStartedTooEarlyCompletesItsCreateAndStarts(t *testing.T) {
	ctx := context.Background()
	ownerID := uuid.New()
	w := roomyAdmissionWorld()
	cfg := coretest.StaticConfig{Config: w.config()}
	store := newContainerStore()
	c := model.Container{
		ID: uuid.New(), OwnerID: ownerID, Name: "web", ImageTag: "nginx:latest",
		Status: model.ContainerStatusPending, DesiredStatus: model.ContainerStatusCreated, DockerGeneration: 1,
		BaseMemoryReservation: 256 * admissionMiB, BaseCPUReservation: 250,
	}
	createOp := model.ResourceOperation{
		ID: uuid.New(), ResourceType: model.ResourceTypeContainer, ResourceID: c.ID, OwnerID: ownerID,
		Operation: model.OperationCreate, Status: model.OperationStatusPending,
	}
	require.NoError(t, store.SaveWithMountsOperationAndOutbox(ctx, c, nil, createOp, model.ContainerLifecycleOutbox{ID: uuid.New(), OperationID: createOp.ID, ContainerID: c.ID}, false, false))
	_, err := store.LeasePendingContainerOutbox(ctx, 10) // the create is published
	require.NoError(t, err)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	dockerAPI.EXPECT().EnsureUserNetwork(mock.Anything, mock.Anything).Return("network", nil).Once()
	dockerAPI.EXPECT().ImageExists(mock.Anything, "nginx:latest").Return(true, nil).Once()
	dockerAPI.EXPECT().CreateContainer(mock.Anything, mock.Anything).Return(createdDocker, nil).Once()
	images := newContainerImageRepository(t)
	images.EXPECT().FindByOwnerAndTag(mock.Anything, mock.Anything, mock.Anything).Return(model.Image{}, false, nil).Maybe()
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:     store,
		Images:         images,
		Docker:         dockerAPI,
		Metrics:        admissionHostMetrics(t),
		DiskMetrics:    admissionDiskMetrics(t, w),
		DockerDataPath: admissionDockerDataPath,
		Config:         cfg,
		Quotas:         w.quotas(t, ownerID, cfg),
		Logger:         slog.New(slog.DiscardHandler),
	})
	userCtx := accessscope.WithUserScope(ctx, ownerID, "", "")

	require.ErrorIs(t, svc.Start(userCtx, c.ID), apperrors.ErrConflict)
	worker := service.NewContainerCreateWorker(svc, cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, worker.HandleMessage(ctx, queue.ContainerLifecycleMessage{
		OperationID: createOp.ID.String(), ContainerID: c.ID.String(), OwnerID: ownerID.String(), Operation: model.OperationCreate,
	}))
	require.NoError(t, svc.Start(userCtx, c.ID))

	queued := requireQueued(t, store)
	require.Equal(t, model.OperationStart, queued.op.Operation)
	require.Equal(t, model.ContainerStatusStarting, queued.container.Status)
	require.Equal(t, createdDocker, queued.container.DockerID)
}

// Start on a running container succeeds and queues nothing, so a second
// click can never reset a boosted memory limit.
func TestStartOnARunningContainerQueuesNothing(t *testing.T) {
	ownerID := uuid.New()
	w := roomyAdmissionWorld()
	store := newContainerStore()
	c := stoppedContainer(ownerID, nil)
	c.Status, c.DesiredStatus = model.ContainerStatusRunning, model.ContainerStatusRunning
	saveContainer(t, store, c)
	svc := admissionContainerService(t, w, ownerID, store, coretest.StaticConfig{Config: w.config()})

	require.NoError(t, svc.Start(accessscope.WithUserScope(context.Background(), ownerID, "", ""), c.ID))

	requireNothingQueued(t, store)
	got, err := store.GetByID(context.Background(), c.ID)
	require.NoError(t, err)
	require.Equal(t, model.ContainerStatusRunning, got.Status)
}

// A crashed container still holds its reservation (its desired status is
// running), so its restart asks for nothing more: 1536 MB of a 2048 MB quota
// restarts, where admitting it again would count it twice.
func TestRestartingACrashedContainerIsAdmittedOnce(t *testing.T) {
	ownerID := uuid.New()
	w := roomyAdmissionWorld()
	w.quotaRAMMB = 2048
	store := newContainerStore()
	c := stoppedContainer(ownerID, nil)
	c.Status, c.DesiredStatus = model.ContainerStatusError, model.ContainerStatusRunning
	c.BaseMemoryReservation = 1536 * admissionMiB
	saveContainer(t, store, c)
	svc := admissionContainerService(t, w, ownerID, store, coretest.StaticConfig{Config: w.config()})

	require.NoError(t, svc.Start(accessscope.WithUserScope(context.Background(), ownerID, "", ""), c.ID))

	queued := requireQueued(t, store)
	require.Equal(t, model.OperationStart, queued.op.Operation)
}

// Start decides from the record it reads under the owner capacity lock: a
// container another request started while this one waited for the lock is
// not started again.
func TestStartDecidesFromTheRecordReadUnderTheCapacityLock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		ownerID := uuid.New()
		w := roomyAdmissionWorld()
		store := newContainerStore()
		c := stoppedContainer(ownerID, nil)
		saveContainer(t, store, c)
		svc := admissionContainerService(t, w, ownerID, store, coretest.StaticConfig{Config: w.config()})
		unlock, err := store.AcquireOwnerCapacityLock(ctx, ownerID)
		require.NoError(t, err)
		done := make(chan error, 1)

		go func() { done <- svc.Start(accessscope.WithUserScope(ctx, ownerID, "", ""), c.ID) }()
		synctest.Wait()
		require.NoError(t, store.UpdateStatus(ctx, c.ID, model.ContainerStatusRunning))
		unlock()

		require.NoError(t, <-done)
		requireNothingQueued(t, store)
	})
}

var _ service.ContainerDockerAPI = (*docker.Adapter)(nil)
