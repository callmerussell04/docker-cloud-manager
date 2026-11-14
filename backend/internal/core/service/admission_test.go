package service_test

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/userquota"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const admissionMiB = int64(1 << 20)

// admissionWorld is the reservation state an admission request meets. The
// owner already runs one 512 MiB, 500m container; another owner's running
// container and active builds take host capacity. The host has 2 GiB and
// 2000m, with no overcommit and nothing held back for the system. Every entry
// point asks for 256 MiB and 250m: container create, container start, project
// start, and build creation through BuildMemoryBytes and the build CPU quota.
// Builder runs up to four builds at once unless a case narrows it, and each
// build slot also holds buildMaxDiskBytes of disk against the 1 GiB disk
// floor. A volume create asks for the default 64 MiB size and no memory or CPU.
//
// The owner's quota is quotaRAMMB, quotaCPU and quotaDiskMB. With customQuota
// it is a custom quota in Core's store and the defaults are one millicore and
// 1 MB, which would reject everything; otherwise it is the default quota and
// the owner has no custom quota.
type admissionWorld struct {
	quotaRAMMB, quotaDiskMB int64
	quotaCPU                float64
	customQuota             bool
	// usedDiskMB is image disk; reservedVolumeBytes is what the owner's
	// volumes reserve.
	usedDiskMB            int64
	reservedVolumeBytes   int64
	otherMemory, otherCPU int64
	activeBuilds          int
	maxConcurrentBuilds   int
	buildMaxDiskBytes     int64
	diskFreeBytes         int64
}

func roomyAdmissionWorld() admissionWorld {
	return admissionWorld{
		quotaRAMMB:    1024,
		quotaCPU:      1,
		quotaDiskMB:   1024,
		otherMemory:   256 * admissionMiB,
		otherCPU:      250,
		diskFreeBytes: 10 << 30,

		maxConcurrentBuilds: 4,
		buildMaxDiskBytes:   1 << 30,
	}
}

func (w admissionWorld) config() config.SystemConfig {
	cfg := containerCreateConfig()
	cfg.MaxQueuedContainerCreatesPerUser = 0
	cfg.MaxQueuedBuildsPerUser = 0
	cfg.ImageBuildsEnabled = true
	cfg.DefaultMemoryReservation = 256 * admissionMiB
	cfg.DefaultCPUReservation = 250
	cfg.ReservedSystemMemory, cfg.ReservedSystemCPU = 0, 0
	cfg.OvercommitFactor, cfg.CPUOvercommitFactor = 1, 1
	cfg.BuildMemoryBytes = 256 * admissionMiB
	cfg.BuildCPUQuota, cfg.BuildCPUPeriod = 25000, 100000
	cfg.MaxConcurrentBuilds = w.maxConcurrentBuilds
	cfg.BuildMaxDiskBytes = w.buildMaxDiskBytes
	cfg.HostMinFreeDiskBytes = 1 << 30
	cfg.DefaultVolumeSizeBytes = 64 * admissionMiB
	cfg.DefaultUserQuotaCPU, cfg.DefaultUserQuotaRAMMB, cfg.DefaultUserQuotaDiskMB = w.quotaCPU, w.quotaRAMMB, w.quotaDiskMB
	if w.customQuota {
		cfg.DefaultUserQuotaCPU, cfg.DefaultUserQuotaRAMMB, cfg.DefaultUserQuotaDiskMB = 0.001, 1, 1
	}
	return cfg
}

// quotas returns the effective quota source for ownerID in w, over Core's
// quota store and cfg.
func (w admissionWorld) quotas(t *testing.T, ownerID uuid.UUID, cfg service.ConfigManager) *userquota.Service {
	t.Helper()
	store := coretest.NewUserQuotaStoreFake()
	if w.customQuota {
		require.NoError(t, store.SetCustomUserQuota(context.Background(), ownerID, model.UserQuota{
			CPUMillicores: config.CPUMillicores(w.quotaCPU), RAMMB: w.quotaRAMMB, DiskMB: w.quotaDiskMB,
		}))
	}
	quotas, err := userquota.New(store, cfg)
	require.NoError(t, err)
	return quotas
}

// admissionEntry runs one request entry point against a world and reports its
// error and whether it queued the work it was asked to admit.
type admissionEntry struct {
	name string
	// chargesUser reports whether the request counts against the owner's
	// memory and CPU quotas. Builds reserve host capacity only.
	chargesUser bool
	// volume marks the volume create, which asks only for disk quota and
	// meets the disk floor.
	volume bool
	run    func(t *testing.T, w admissionWorld) (queued bool, err error)
}

var admissionEntries = []admissionEntry{
	{name: "container create", chargesUser: true, run: admitContainerCreate},
	{name: "container start", chargesUser: true, run: admitContainerStart},
	{name: "project start", chargesUser: true, run: admitProjectStart},
	{name: "build creation", chargesUser: false, run: admitBuildCreation},
	{name: "volume create", volume: true, run: admitVolumeCreate},
}

// Every entry point admits memory and CPU through one check, in the order
// user memory, user CPU, user disk, host memory including build slots, host
// CPU including build slots, disk floor, so the same over-limit request is rejected
// with the same error kind wherever it arrives. Each verdict holds whether the
// owner's quota is the default quota or a custom quota in Core's store.
func TestAdmissionThroughEveryEntryPoint(t *testing.T) {
	for _, tc := range []struct {
		name  string
		world func(*admissionWorld)
		// want is the error kind for requests charged to the user; wantBuild,
		// for builds, is the same unless the limit is a user memory or CPU
		// quota, which builds do not count against. wantVolume is for volume
		// creates, which meet only the disk quota and the disk floor.
		want, wantBuild, wantVolume error
	}{
		{name: "fits", world: func(*admissionWorld) {}},
		{
			name:  "user memory quota",
			world: func(w *admissionWorld) { w.quotaRAMMB = 700 },
			want:  apperrors.ErrQuotaExceeded,
		},
		{
			name:  "user cpu quota",
			world: func(w *admissionWorld) { w.quotaCPU = 0.6 },
			want:  apperrors.ErrQuotaExceeded,
		},
		{
			name:  "user disk quota",
			world: func(w *admissionWorld) { w.usedDiskMB = 1024 },
			want:  apperrors.ErrQuotaExceeded, wantBuild: apperrors.ErrQuotaExceeded, wantVolume: apperrors.ErrQuotaExceeded,
		},
		{
			// Measured usage is nothing, but the sizes the owner's volumes
			// reserve fill the quota.
			name:  "volume sizes fill the user disk quota",
			world: func(w *admissionWorld) { w.reservedVolumeBytes = 1024 * admissionMiB },
			want:  apperrors.ErrQuotaExceeded, wantBuild: apperrors.ErrQuotaExceeded, wantVolume: apperrors.ErrQuotaExceeded,
		},
		{
			// 1000 of 1024 MB leaves room for a container or a build, but
			// not for a 64 MiB volume.
			name:       "a volume needs room for its size",
			world:      func(w *admissionWorld) { w.usedDiskMB = 1000 },
			wantVolume: apperrors.ErrQuotaExceeded,
		},
		{
			name:  "a volume that fills the quota exactly",
			world: func(w *admissionWorld) { w.usedDiskMB, w.reservedVolumeBytes = 900, 60*admissionMiB },
		},
		{
			name:  "host memory",
			world: func(w *admissionWorld) { w.otherMemory = 1400 * admissionMiB },
			want:  apperrors.ErrHostExhausted, wantBuild: apperrors.ErrHostExhausted,
		},
		{
			name: "host memory held by active builds",
			world: func(w *admissionWorld) {
				w.otherMemory = 1000 * admissionMiB
				w.activeBuilds = 2
			},
			want: apperrors.ErrHostExhausted, wantBuild: apperrors.ErrHostExhausted,
		},
		{
			name:  "host cpu",
			world: func(w *admissionWorld) { w.otherCPU = 1300 },
			want:  apperrors.ErrHostExhausted, wantBuild: apperrors.ErrHostExhausted,
		},
		{
			name: "host cpu held by active builds",
			world: func(w *admissionWorld) {
				w.otherCPU = 800
				w.activeBuilds = 2
			},
			want: apperrors.ErrHostExhausted, wantBuild: apperrors.ErrHostExhausted,
		},
		{
			// One build runs at a time, so the five queued behind it hold
			// nothing: the same reservations that two running builds exhaust
			// the host with still fit.
			name: "builds queued behind the concurrency limit reserve nothing",
			world: func(w *admissionWorld) {
				w.otherMemory = 1000 * admissionMiB
				w.otherCPU = 800
				w.activeBuilds = 6
				w.maxConcurrentBuilds = 1
			},
		},
		{
			name:  "disk floor",
			world: func(w *admissionWorld) { w.diskFreeBytes = 512 * admissionMiB },
			want:  apperrors.ErrHostExhausted, wantBuild: apperrors.ErrHostExhausted, wantVolume: apperrors.ErrHostExhausted,
		},
		{
			// Two running builds and the new one may each write 3 GiB: 9 of
			// the 10 GiB free leaves exactly the 1 GiB floor.
			name: "build slots fill the disk down to the floor",
			world: func(w *admissionWorld) {
				w.activeBuilds = 2
				w.buildMaxDiskBytes = 3 << 30
			},
		},
		{
			// 12 GiB for three slots goes below the floor, so a build is
			// rejected; other work writes nothing of its own and fits.
			name: "disk floor held by build slots",
			world: func(w *admissionWorld) {
				w.activeBuilds = 2
				w.buildMaxDiskBytes = 4 << 30
			},
			wantBuild: apperrors.ErrHostExhausted,
		},
		{
			// One build runs at a time, so the new build waits behind it and
			// asks for no disk beyond that one slot's 4 GiB.
			name: "builds queued behind the concurrency limit hold no disk",
			world: func(w *admissionWorld) {
				w.activeBuilds = 6
				w.maxConcurrentBuilds = 1
				w.buildMaxDiskBytes = 4 << 30
			},
		},
		{
			name: "a build bigger than the free disk above the floor",
			world: func(w *admissionWorld) {
				w.buildMaxDiskBytes = 9<<30 + 1
			},
			wantBuild: apperrors.ErrHostExhausted,
		},
		{
			name: "user disk is checked before host memory",
			world: func(w *admissionWorld) {
				w.usedDiskMB = 1024
				w.otherMemory = 1400 * admissionMiB
			},
			want: apperrors.ErrQuotaExceeded, wantBuild: apperrors.ErrQuotaExceeded, wantVolume: apperrors.ErrQuotaExceeded,
		},
		{
			name: "user memory is checked before the disk floor",
			world: func(w *admissionWorld) {
				w.quotaRAMMB = 700
				w.diskFreeBytes = 0
			},
			want: apperrors.ErrQuotaExceeded, wantBuild: apperrors.ErrHostExhausted, wantVolume: apperrors.ErrHostExhausted,
		},
	} {
		for _, source := range []struct {
			name   string
			custom bool
		}{{name: "default quota"}, {name: "custom quota", custom: true}} {
			for _, entry := range admissionEntries {
				t.Run(tc.name+"/"+source.name+"/"+entry.name, func(t *testing.T) {
					w := roomyAdmissionWorld()
					w.customQuota = source.custom
					tc.world(&w)
					want := tc.want
					switch {
					case entry.volume:
						want = tc.wantVolume
					case !entry.chargesUser:
						want = tc.wantBuild
					}

					queued, err := entry.run(t, w)

					if want == nil {
						require.NoError(t, err)
						require.True(t, queued, "admitted work is queued")
						return
					}
					require.ErrorIs(t, err, want)
					require.False(t, queued, "rejected work is not queued")
				})
			}
		}
	}
}

// Defaults are read at each admission: after an administrator lowers the
// default quota, the next request of an owner on the default quota is
// rejected and that of an owner with a custom quota is still admitted, by the
// same service.
func TestChangingADefaultQuotaChangesOnlyOwnersWithoutACustomQuota(t *testing.T) {
	type entryPoint struct {
		name string
		// admit builds one service for ownerID that reads cfg at each
		// request, and returns a function making the next request.
		admit func(t *testing.T, w admissionWorld, ownerID uuid.UUID, cfg service.ConfigManager) func(n int) error
	}
	entries := []entryPoint{
		{name: "container create", admit: func(t *testing.T, w admissionWorld, ownerID uuid.UUID, cfg service.ConfigManager) func(int) error {
			svc := admissionContainerService(t, w, ownerID, admissionStore(t, w, ownerID), cfg)
			ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
			return func(n int) error {
				_, err := svc.Create(ctx, model.ContainerCreateParams{Name: fmt.Sprintf("web-%d", n), ImageTag: "nginx:latest", RequestedMemoryMB: 256})
				return err
			}
		}},
		{name: "volume create", admit: func(t *testing.T, w admissionWorld, ownerID uuid.UUID, cfg service.ConfigManager) func(int) error {
			svc, _ := admissionVolumeService(t, w, ownerID, admissionStore(t, w, ownerID), cfg)
			ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
			return func(n int) error {
				_, err := svc.Create(ctx, model.VolumeCreateParams{Name: fmt.Sprintf("data-%d", n)})
				return err
			}
		}},
	}
	for _, entry := range entries {
		for _, tc := range []struct {
			name      string
			custom    bool
			wantAfter error
		}{
			{name: "default quota", wantAfter: apperrors.ErrQuotaExceeded},
			{name: "custom quota", custom: true},
		} {
			t.Run(entry.name+"/"+tc.name, func(t *testing.T) {
				w := roomyAdmissionWorld()
				w.customQuota = tc.custom
				ownerID := uuid.New()
				current := w.config()
				current.DefaultUserQuotaCPU, current.DefaultUserQuotaRAMMB, current.DefaultUserQuotaDiskMB = 1, 1024, 1024
				cfg := coremocks.NewConfigManager(t)
				cfg.EXPECT().Get().RunAndReturn(func() config.SystemConfig { return current })
				request := entry.admit(t, w, ownerID, cfg)

				require.NoError(t, request(1), "both owners fit the roomy defaults")

				current.DefaultUserQuotaCPU, current.DefaultUserQuotaRAMMB, current.DefaultUserQuotaDiskMB = 0.5, 512, 32
				err := request(2)

				if tc.wantAfter == nil {
					require.NoError(t, err, "a custom quota does not follow the defaults")
					return
				}
				require.ErrorIs(t, err, tc.wantAfter)
			})
		}
	}
}

// admissionStore returns a container store holding the owner's running
// container and another owner's.
func admissionStore(t *testing.T, w admissionWorld, ownerID uuid.UUID) *containerStore {
	t.Helper()
	store := newContainerStore()
	for _, c := range []model.Container{
		{OwnerID: ownerID, BaseMemoryReservation: 512 * admissionMiB, BaseCPUReservation: 500},
		{OwnerID: uuid.New(), BaseMemoryReservation: w.otherMemory, BaseCPUReservation: w.otherCPU},
	} {
		c.ID = uuid.New()
		c.Name = "running-" + c.ID.String()[:8]
		c.DockerID = "docker-" + c.ID.String()[:12]
		c.Status, c.DesiredStatus = model.ContainerStatusRunning, model.ContainerStatusRunning
		saveContainer(t, store, c)
	}
	return store
}

// admissionContainerService builds a container service over store whose
// quotas, images, host and builds report w, reading cfg.
func admissionContainerService(t *testing.T, w admissionWorld, ownerID uuid.UUID, store *containerStore, cfg service.ConfigManager) *service.ContainerService {
	t.Helper()
	images := coremocks.NewContainerImageRepository(t)
	images.EXPECT().GetUserImageBytes(mock.Anything, ownerID).Return(w.usedDiskMB<<20, nil).Maybe()
	images.EXPECT().FindByOwnerAndTag(mock.Anything, mock.Anything, mock.Anything).Return(model.Image{}, false, nil).Maybe()
	builds := coremocks.NewActiveSystemBuildCounter(t)
	builds.EXPECT().CountActive(mock.Anything).Return(w.activeBuilds, nil).Maybe()
	volumes := coremocks.NewContainerVolumeRepository(t)
	volumes.EXPECT().GetUserReservedVolumeBytes(mock.Anything, ownerID).Return(w.reservedVolumeBytes, nil).Maybe()
	return newTestContainerService(t, service.ContainerServiceDeps{
		Repository:  store,
		Volumes:     volumes,
		Images:      images,
		Metrics:     admissionHostMetrics(t),
		DiskMetrics: admissionDiskMetrics(t, w),
		Config:      cfg,
		Quotas:      w.quotas(t, ownerID, cfg),
		Builds:      builds,
		Logger:      slog.New(slog.DiscardHandler),
	})
}

// admissionVolumeService builds a volume service whose owner has no volumes
// yet, under the owner capacity lock that store holds. The lifecycle fake
// reports what was queued.
func admissionVolumeService(t *testing.T, w admissionWorld, ownerID uuid.UUID, store *containerStore, cfg service.ConfigManager) (*service.VolumeService, *volumeLifecycleFake) {
	t.Helper()
	images := coremocks.NewVolumeImageDiskRepository(t)
	images.EXPECT().GetUserImageBytes(mock.Anything, ownerID).Return(w.usedDiskMB<<20, nil).Maybe()
	repo := newVolumeRepoMock(t)
	repo.EXPECT().GetUserReservedVolumeBytes(mock.Anything, ownerID).Return(w.reservedVolumeBytes, nil).Maybe()
	repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(0, nil).Maybe()
	lifecycle := &volumeLifecycleFake{}
	svc := newTestVolumeService(t, service.VolumeServiceDeps{
		Repository:   repo,
		Docker:       coremocks.NewVolumeDockerAPI(t),
		Config:       cfg,
		Lifecycle:    lifecycle,
		Quotas:       w.quotas(t, ownerID, cfg),
		ImageRepo:    images,
		DiskMetrics:  admissionDiskMetrics(t, w),
		CapacityLock: store,
		Logger:       slog.New(slog.DiscardHandler),
	})
	return svc, lifecycle
}

func admissionHostMetrics(t *testing.T) *coremocks.HostMetricsProvider {
	t.Helper()
	metrics := coremocks.NewHostMetricsProvider(t)
	metrics.EXPECT().GetTotalMemory().Return(2048*admissionMiB, nil).Maybe()
	metrics.EXPECT().GetLogicalCPUs().Return(int64(2), nil).Maybe()
	return metrics
}

func admissionDiskMetrics(t *testing.T, w admissionWorld) *coremocks.HostDiskMetricsProvider {
	t.Helper()
	disk := coremocks.NewHostDiskMetricsProvider(t)
	disk.EXPECT().GetDiskUsage(mock.Anything).Return(model.HostDiskStats{FreeBytes: w.diskFreeBytes}, nil).Maybe()
	return disk
}

// stoppedContainer is the owner's exited 256 MiB, 250m container.
func stoppedContainer(ownerID uuid.UUID, projectID *uuid.UUID) model.Container {
	id := uuid.New()
	return model.Container{
		ID:                    id,
		OwnerID:               ownerID,
		ProjectID:             projectID,
		Name:                  "stopped-" + id.String()[:8],
		DockerID:              "docker-" + id.String()[:12],
		Status:                model.ContainerStatusExited,
		DesiredStatus:         model.ContainerStatusExited,
		BaseMemoryReservation: 256 * admissionMiB,
		BaseCPUReservation:    250,
	}
}

func admitContainerCreate(t *testing.T, w admissionWorld) (bool, error) {
	ownerID := uuid.New()
	store := admissionStore(t, w, ownerID)
	svc := admissionContainerService(t, w, ownerID, store, coretest.StaticConfig{Config: w.config()})
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")

	_, err := svc.Create(ctx, model.ContainerCreateParams{Name: "web", ImageTag: "nginx:latest", RequestedMemoryMB: 256})

	return queuedCount(t, store) == 1, err
}

func admitContainerStart(t *testing.T, w admissionWorld) (bool, error) {
	ownerID := uuid.New()
	store := admissionStore(t, w, ownerID)
	c := stoppedContainer(ownerID, nil)
	saveContainer(t, store, c)
	svc := admissionContainerService(t, w, ownerID, store, coretest.StaticConfig{Config: w.config()})
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")

	err := svc.Start(ctx, c.ID)

	return queuedCount(t, store) == 1, err
}

func admitProjectStart(t *testing.T, w admissionWorld) (bool, error) {
	ownerID, projectID := uuid.New(), uuid.New()
	store := admissionStore(t, w, ownerID)
	c := stoppedContainer(ownerID, &projectID)
	saveContainer(t, store, c)
	projects := coremocks.NewProjectRepository(t)
	projects.EXPECT().GetByID(mock.Anything, projectID).Return(model.Project{ID: projectID, OwnerID: ownerID, Status: model.ProjectStatusStopped}, nil)
	queued := false
	projects.EXPECT().UpdateStatus(mock.Anything, projectID, model.ProjectStatusStarting, (*string)(nil)).
		RunAndReturn(func(context.Context, uuid.UUID, string, *string) error {
			queued = true
			return nil
		}).Maybe()
	resources := coremocks.NewProjectResourceRepository(t)
	resources.EXPECT().GetByProjectID(mock.Anything, projectID).RunAndReturn(func(ctx context.Context, _ uuid.UUID) ([]model.Container, error) {
		got, err := store.GetByID(ctx, c.ID)
		return []model.Container{got}, err
	})
	cfg := coretest.StaticConfig{Config: w.config()}
	svc := newTestProjectService(t, service.ProjectServiceDeps{
		Repository: projects,
		Resources:  resources,
		Capacity:   admissionContainerService(t, w, ownerID, store, cfg),
		Config:     cfg,
	})
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")

	err := svc.Start(ctx, projectID)

	return queued, err
}

func admitBuildCreation(t *testing.T, w admissionWorld) (bool, error) {
	ownerID := uuid.New()
	store := admissionStore(t, w, ownerID)
	deps := buildServiceDeps(t)
	cfg := coretest.StaticConfig{Config: w.config()}
	deps.Config = cfg
	deps.Containers = store
	deps.HostMetrics = admissionHostMetrics(t)
	deps.DiskMetrics = admissionDiskMetrics(t, w)
	deps.Quotas = w.quotas(t, ownerID, cfg)
	images := coremocks.NewBuildImageRepository(t)
	images.EXPECT().GetUserImageBytes(mock.Anything, ownerID).Return(w.usedDiskMB<<20, nil).Maybe()
	deps.Images = images
	volumes := coremocks.NewBuildVolumeDiskRepository(t)
	volumes.EXPECT().GetUserReservedVolumeBytes(mock.Anything, ownerID).Return(w.reservedVolumeBytes, nil).Maybe()
	deps.VolumeRepo = volumes
	repo := coremocks.NewBuildRepository(t)
	repo.EXPECT().AcquireCapacityLock(mock.Anything).Return(func() {}, nil).Once()
	repo.EXPECT().CountActive(mock.Anything).Return(w.activeBuilds, nil).Maybe()
	queued := false
	repo.EXPECT().CreateQueuedBuild(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(context.Context, model.Image, model.Build, model.BuildQueueOutbox) error {
			queued = true
			return nil
		}).Maybe()
	deps.Repository = repo
	svc, err := service.NewBuildService(deps)
	require.NoError(t, err)
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")

	_, _, err = svc.CreateBuildJob(ctx, "demo", "archive.zip", "build.log", ".", "Dockerfile", nil, "request")

	return queued, err
}

// admitVolumeCreate creates a volume of the default size under the owner
// capacity lock that store holds.
func admitVolumeCreate(t *testing.T, w admissionWorld) (bool, error) {
	ownerID := uuid.New()
	store := admissionStore(t, w, ownerID)
	svc, lifecycle := admissionVolumeService(t, w, ownerID, store, coretest.StaticConfig{Config: w.config()})
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")

	_, err := svc.Create(ctx, model.VolumeCreateParams{Name: "data"})

	return lifecycle.createQueued, err
}

// queuedCount reports how many lifecycle operations store holds queued.
func queuedCount(t *testing.T, store *containerStore) int {
	t.Helper()
	outbox, err := store.LeasePendingContainerOutbox(context.Background(), 10)
	require.NoError(t, err)
	return len(outbox)
}

// Two creates for one owner race for its last container slot. The first holds
// the owner capacity lock while it reads the reservation ledger; the second
// waits for the lock instead of counting the owner's containers beside it, so
// only one is admitted.
func TestConcurrentContainerCreatesAreSerialisedByTheCapacityLock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := roomyAdmissionWorld()
		cfg := w.config()
		cfg.MaxContainersPerUser = 1
		ownerID := uuid.New()
		store := newContainerStore()

		gate := make(chan struct{})
		var mu sync.Mutex
		ledgerReads := 0
		images := coremocks.NewContainerImageRepository(t)
		images.EXPECT().GetUserImageBytes(mock.Anything, ownerID).RunAndReturn(func(context.Context, uuid.UUID) (int64, error) {
			mu.Lock()
			ledgerReads++
			first := ledgerReads == 1
			mu.Unlock()
			if first {
				<-gate
			}
			return 0, nil
		})
		images.EXPECT().FindByOwnerAndTag(mock.Anything, mock.Anything, mock.Anything).Return(model.Image{}, false, nil).Maybe()
		static := coretest.StaticConfig{Config: cfg}
		svc := newTestContainerService(t, service.ContainerServiceDeps{
			Repository:  store,
			Images:      images,
			Metrics:     admissionHostMetrics(t),
			DiskMetrics: admissionDiskMetrics(t, w),
			Config:      static,
			Quotas:      w.quotas(t, ownerID, static),
			Logger:      slog.New(slog.DiscardHandler),
		})
		ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")

		errs := make(chan error, 2)
		create := func(name string) {
			_, err := svc.Create(ctx, model.ContainerCreateParams{Name: name, ImageTag: "nginx:latest", RequestedMemoryMB: 256})
			errs <- err
		}
		go create("first")
		synctest.Wait()
		go create("second")
		synctest.Wait()
		mu.Lock()
		require.Equal(t, 1, ledgerReads, "the second create waits for the lock the first holds")
		mu.Unlock()

		close(gate)
		results := []error{<-errs, <-errs}

		admitted := 0
		for _, err := range results {
			if err == nil {
				admitted++
				continue
			}
			require.ErrorIs(t, err, apperrors.ErrLimitExceeded)
		}
		require.Equal(t, 1, admitted, "one create is admitted")
		count, err := store.CountByOwnerID(context.Background(), ownerID)
		require.NoError(t, err)
		require.Equal(t, 1, count, "the owner holds one container")
		require.Equal(t, 1, queuedCount(t, store), "one create is queued")
	})
}
