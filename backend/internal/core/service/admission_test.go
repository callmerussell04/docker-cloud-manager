package service_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
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
// Builder runs up to four builds at once unless a case narrows it.
type admissionWorld struct {
	quotaRAMMB, quotaDiskMB int64
	quotaCPU                float64
	usedDiskMB              int64
	otherMemory, otherCPU   int64
	activeBuilds            int
	maxConcurrentBuilds     int
	diskFreeBytes           int64
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
	cfg.HostMinFreeDiskBytes = 1 << 30
	return cfg
}

// admissionEntry runs one request entry point against a world and reports its
// error and whether it queued the work it was asked to admit.
type admissionEntry struct {
	name string
	// chargesUser reports whether the request counts against the owner's
	// memory and CPU quotas. Builds reserve host capacity only.
	chargesUser bool
	run         func(t *testing.T, w admissionWorld) (queued bool, err error)
}

var admissionEntries = []admissionEntry{
	{name: "container create", chargesUser: true, run: admitContainerCreate},
	{name: "container start", chargesUser: true, run: admitContainerStart},
	{name: "project start", chargesUser: true, run: admitProjectStart},
	{name: "build creation", chargesUser: false, run: admitBuildCreation},
}

// Every entry point admits memory and CPU through one check, in the order
// user memory, user CPU, user disk, host memory including build slots, host
// CPU including build slots, disk floor, so the same over-limit request is rejected
// with the same error kind wherever it arrives.
func TestAdmissionThroughEveryEntryPoint(t *testing.T) {
	for _, tc := range []struct {
		name  string
		world func(*admissionWorld)
		// want is the error kind for requests charged to the user; wantBuild,
		// for builds, is the same unless the limit is a user memory or CPU
		// quota, which builds do not count against.
		want, wantBuild error
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
			want:  apperrors.ErrQuotaExceeded, wantBuild: apperrors.ErrQuotaExceeded,
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
			want:  apperrors.ErrHostExhausted, wantBuild: apperrors.ErrHostExhausted,
		},
		{
			name: "user disk is checked before host memory",
			world: func(w *admissionWorld) {
				w.usedDiskMB = 1024
				w.otherMemory = 1400 * admissionMiB
			},
			want: apperrors.ErrQuotaExceeded, wantBuild: apperrors.ErrQuotaExceeded,
		},
		{
			name: "user memory is checked before the disk floor",
			world: func(w *admissionWorld) {
				w.quotaRAMMB = 700
				w.diskFreeBytes = 0
			},
			want: apperrors.ErrQuotaExceeded, wantBuild: apperrors.ErrHostExhausted,
		},
	} {
		for _, entry := range admissionEntries {
			t.Run(tc.name+"/"+entry.name, func(t *testing.T) {
				w := roomyAdmissionWorld()
				tc.world(&w)
				want := tc.want
				if !entry.chargesUser {
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
// users, images, host and builds report w.
func admissionContainerService(t *testing.T, w admissionWorld, ownerID uuid.UUID, store *containerStore) *service.ContainerService {
	t.Helper()
	users := coremocks.NewUserInfoProvider(t)
	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{
		ID: ownerID, QuotaRAMMB: w.quotaRAMMB, QuotaCPU: w.quotaCPU, QuotaDiskMB: w.quotaDiskMB,
	}, nil).Maybe()
	images := coremocks.NewContainerImageRepository(t)
	images.EXPECT().GetUserUsedDiskSpace(mock.Anything, ownerID).Return(w.usedDiskMB, nil).Maybe()
	images.EXPECT().List(mock.Anything, mock.Anything).Return(nil, 0, nil).Maybe()
	builds := coremocks.NewActiveSystemBuildCounter(t)
	builds.EXPECT().CountActive(mock.Anything).Return(w.activeBuilds, nil).Maybe()
	return newTestContainerService(t, service.ContainerServiceDeps{
		Repository:  store,
		Images:      images,
		Metrics:     admissionHostMetrics(t),
		DiskMetrics: admissionDiskMetrics(t, w),
		Config:      coretest.StaticConfig{Config: w.config()},
		Users:       users,
		Builds:      builds,
		Logger:      slog.New(slog.DiscardHandler),
	})
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
	svc := admissionContainerService(t, w, ownerID, store)
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")

	_, err := svc.Create(ctx, model.ContainerCreateParams{Name: "web", ImageTag: "nginx:latest", RequestedMemoryMB: 256})

	return queuedCount(t, store) == 1, err
}

func admitContainerStart(t *testing.T, w admissionWorld) (bool, error) {
	ownerID := uuid.New()
	store := admissionStore(t, w, ownerID)
	c := stoppedContainer(ownerID, nil)
	saveContainer(t, store, c)
	svc := admissionContainerService(t, w, ownerID, store)
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
	svc := newTestProjectService(t, service.ProjectServiceDeps{
		Repository: projects,
		Resources:  resources,
		Capacity:   admissionContainerService(t, w, ownerID, store),
		Config:     coretest.StaticConfig{Config: w.config()},
	})
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")

	err := svc.Start(ctx, projectID)

	return queued, err
}

func admitBuildCreation(t *testing.T, w admissionWorld) (bool, error) {
	ownerID := uuid.New()
	store := admissionStore(t, w, ownerID)
	deps := buildServiceDeps(t)
	deps.Config = coretest.StaticConfig{Config: w.config()}
	deps.Containers = store
	deps.HostMetrics = admissionHostMetrics(t)
	deps.DiskMetrics = admissionDiskMetrics(t, w)
	users := coremocks.NewUserInfoProvider(t)
	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{
		ID: ownerID, QuotaRAMMB: w.quotaRAMMB, QuotaCPU: w.quotaCPU, QuotaDiskMB: w.quotaDiskMB,
	}, nil).Maybe()
	deps.Users = users
	images := coremocks.NewBuildImageRepository(t)
	images.EXPECT().GetUserUsedDiskSpace(mock.Anything, ownerID).Return(w.usedDiskMB, nil).Maybe()
	deps.Images = images
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
		users := coremocks.NewUserInfoProvider(t)
		users.EXPECT().GetUser(mock.Anything, ownerID).RunAndReturn(func(context.Context, uuid.UUID) (model.UserInfo, error) {
			mu.Lock()
			ledgerReads++
			first := ledgerReads == 1
			mu.Unlock()
			if first {
				<-gate
			}
			return model.UserInfo{ID: ownerID, QuotaRAMMB: w.quotaRAMMB, QuotaCPU: w.quotaCPU, QuotaDiskMB: w.quotaDiskMB}, nil
		})
		images := coremocks.NewContainerImageRepository(t)
		images.EXPECT().GetUserUsedDiskSpace(mock.Anything, ownerID).Return(int64(0), nil).Maybe()
		images.EXPECT().List(mock.Anything, mock.Anything).Return(nil, 0, nil).Maybe()
		svc := newTestContainerService(t, service.ContainerServiceDeps{
			Repository:  store,
			Images:      images,
			Metrics:     admissionHostMetrics(t),
			DiskMetrics: admissionDiskMetrics(t, w),
			Config:      coretest.StaticConfig{Config: cfg},
			Users:       users,
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
