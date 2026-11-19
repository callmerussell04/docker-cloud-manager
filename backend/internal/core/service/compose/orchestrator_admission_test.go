package compose_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"slices"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/userquota"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/gitsource"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	composemocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service/compose"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const planMiB = int64(1 << 20)

// planCompose builds web from the archive and mounts the managed volume
// data into it; cache runs an image. Both services take the default
// reservations.
const planCompose = `
services:
  web:
    build:
      context: .
    volumes:
      - data:/data
  cache:
    image: redis:latest
volumes:
  data:
`

// planComposeWithoutBuild is planCompose with web running an image.
const planComposeWithoutBuild = `
services:
  web:
    image: nginx:latest
    volumes:
      - data:/data
  cache:
    image: redis:latest
volumes:
  data:
`

// planWorld is what a deployment of planCompose meets. Its two services ask
// for 256 MiB and 250m each beside the owner's running 512 MiB, 500m
// container, which fills the 1024 MB and one-core quota exactly. The new
// 64 MiB volume data fits the 1024 MB disk quota. The owner holds one of two
// queued deployments, one of three containers, one of two volumes (logs) and
// one of two queued builds, so each limit is met exactly. The host has
// 4 GiB and four CPUs, nothing held back for the system, no overcommit, and
// one 256 MiB, 250m build slot occupied by the owner's build; 10 GiB of disk
// is free above a 1 GiB floor.
type planWorld struct {
	queuedDeployments int
	// stoppedContainers are the owner's containers beside the running one.
	stoppedContainers     int
	volumeNames           []string
	queuedBuilds          int
	quotaRAMMB            int64
	quotaCPU              float64
	quotaDiskMB           int64
	usedDiskMB            int64
	otherMemory, otherCPU int64
	diskFreeBytes         int64
	unusedPromiseBytes    int64
	withoutBuild          bool
}

func roomyPlanWorld() planWorld {
	return planWorld{
		queuedDeployments: 1,
		volumeNames:       []string{"logs"},
		queuedBuilds:      1,
		quotaRAMMB:        1024,
		quotaCPU:          1,
		quotaDiskMB:       1024,
		diskFreeBytes:     10 << 30,
	}
}

func (w planWorld) config() config.SystemConfig {
	cfg := composeTestConfig()
	cfg.ImageBuildsEnabled = true
	cfg.MaxQueuedComposeDeploysPerUser = 2
	cfg.MaxContainersPerUser = 3
	cfg.MaxVolumesPerUser = 2
	cfg.MaxQueuedBuildsPerUser = 2
	cfg.DefaultMemoryReservation, cfg.DefaultCPUReservation = 256*planMiB, 250
	cfg.DefaultVolumeSizeBytes = 64 * planMiB
	cfg.OvercommitFactor, cfg.CPUOvercommitFactor = 1, 1
	cfg.MaxConcurrentBuilds = 4
	cfg.BuildMemoryBytes, cfg.BuildCPUMillicores = 256*planMiB, 250
	cfg.HostMinFreeDiskBytes = 1 << 30
	cfg.DefaultUserQuotaRAMMB, cfg.DefaultUserQuotaCPU, cfg.DefaultUserQuotaDiskMB = w.quotaRAMMB, w.quotaCPU, w.quotaDiskMB
	return cfg
}

func (w planWorld) compose() string {
	if w.withoutBuild {
		return planComposeWithoutBuild
	}
	return planCompose
}

// Ingestion admits the plan's whole demand and rejects at the first limit
// exceeded, in the order queued deployments, containers, volumes, queued
// builds, then user memory, CPU and disk, host memory and CPU and the disk
// floor. A rejected deployment queues no job and deletes its staged source.
func TestStartDeploymentAdmitsTheWholePlan(t *testing.T) {
	for _, tc := range []struct {
		name  string
		world func(*planWorld)
		want  apperrors.Limit
	}{
		{name: "fits every limit exactly", world: func(*planWorld) {}},
		{
			name:  "queued compose deployments",
			world: func(w *planWorld) { w.queuedDeployments = 2 },
			want:  apperrors.LimitQueuedComposeDeployments,
		},
		{
			name:  "containers per user",
			world: func(w *planWorld) { w.stoppedContainers = 1 },
			want:  apperrors.LimitContainersPerUser,
		},
		{
			name:  "volumes per user",
			world: func(w *planWorld) { w.volumeNames = []string{"logs", "cache"} },
			want:  apperrors.LimitVolumesPerUser,
		},
		{
			name:  "queued builds",
			world: func(w *planWorld) { w.queuedBuilds = 2 },
			want:  apperrors.LimitQueuedBuilds,
		},
		{
			name:  "user memory",
			world: func(w *planWorld) { w.quotaRAMMB = 1000 },
			want:  apperrors.LimitUserMemory,
		},
		{
			name:  "user cpu",
			world: func(w *planWorld) { w.quotaCPU = 0.9 },
			want:  apperrors.LimitUserCPU,
		},
		{
			// 1000 MB of images leave no room for the 64 MiB volume.
			name:  "user disk",
			world: func(w *planWorld) { w.usedDiskMB = 1000 },
			want:  apperrors.LimitUserDisk,
		},
		{
			name:  "host memory",
			world: func(w *planWorld) { w.otherMemory = 3000 * planMiB },
			want:  apperrors.LimitHostMemory,
		},
		{
			name:  "host cpu",
			world: func(w *planWorld) { w.otherCPU = 3000 },
			want:  apperrors.LimitHostCPU,
		},
		{
			// 9 GiB of the 10 GiB free is promised to volumes and unused, so
			// the new volume would go below the 1 GiB floor.
			name:  "disk floor by the unused volume promise",
			world: func(w *planWorld) { w.unusedPromiseBytes = 9 << 30 },
			want:  apperrors.LimitDiskFloor,
		},
		{
			name:  "disk floor",
			world: func(w *planWorld) { w.diskFreeBytes = 512 * planMiB },
			want:  apperrors.LimitDiskFloor,
		},
		{
			name: "queued deployments are checked before containers",
			world: func(w *planWorld) {
				w.queuedDeployments = 2
				w.stoppedContainers = 1
			},
			want: apperrors.LimitQueuedComposeDeployments,
		},
		{
			name: "containers are checked before volumes",
			world: func(w *planWorld) {
				w.stoppedContainers = 1
				w.volumeNames = []string{"logs", "cache"}
			},
			want: apperrors.LimitContainersPerUser,
		},
		{
			name: "volumes are checked before queued builds",
			world: func(w *planWorld) {
				w.volumeNames = []string{"logs", "cache"}
				w.queuedBuilds = 2
			},
			want: apperrors.LimitVolumesPerUser,
		},
		{
			name: "queued builds are checked before user memory",
			world: func(w *planWorld) {
				w.queuedBuilds = 2
				w.quotaRAMMB = 1000
			},
			want: apperrors.LimitQueuedBuilds,
		},
		{
			// The owner already has the volume proj_data, which the parser
			// names data in project proj, so the plan reuses
			// it: it creates no volume, so neither the volume limit, nor the
			// 24 MB left of the disk quota, nor the unused volume promise
			// that leaves no room for a new volume rejects it.
			name: "a volume the owner already has by name is not counted",
			world: func(w *planWorld) {
				w.volumeNames = []string{"logs", "proj_data"}
				w.usedDiskMB = 1000
				w.unusedPromiseBytes = 9 << 30
			},
		},
		{
			name: "a plan without builds meets no queued builds limit",
			world: func(w *planWorld) {
				w.withoutBuild = true
				w.queuedBuilds = 2
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := roomyPlanWorld()
			tc.world(&w)
			h := newPlanHarness(t, w)

			_, err := h.orchestrator.StartDeployment(h.ctx, "proj", "source.zip", "", bytes.NewReader(planArchive(t, w.compose())))

			if tc.want == "" {
				require.NoError(t, err)
				require.Equal(t, w.queuedDeployments+1, h.deployments.count(), "the admitted deployment is queued")
				return
			}
			require.ErrorIs(t, err, planLimitKinds[tc.want])
			require.Equal(t, tc.want, apperrors.LimitOf(err), "the rejection names its limit")
			require.Equal(t, w.queuedDeployments, h.deployments.count(), "a rejected deployment is not queued")
			require.Zero(t, h.objects.Len(), "a rejected deployment deletes its staged source")
		})
	}
}

// planLimitKinds is the error kind each limit answers with.
var planLimitKinds = map[apperrors.Limit]error{
	apperrors.LimitQueuedComposeDeployments: apperrors.ErrLimitExceeded,
	apperrors.LimitContainersPerUser:        apperrors.ErrLimitExceeded,
	apperrors.LimitVolumesPerUser:           apperrors.ErrLimitExceeded,
	apperrors.LimitQueuedBuilds:             apperrors.ErrLimitExceeded,
	apperrors.LimitUserMemory:               apperrors.ErrQuotaExceeded,
	apperrors.LimitUserCPU:                  apperrors.ErrQuotaExceeded,
	apperrors.LimitUserDisk:                 apperrors.ErrQuotaExceeded,
	apperrors.LimitHostMemory:               apperrors.ErrHostExhausted,
	apperrors.LimitHostCPU:                  apperrors.ErrHostExhausted,
	apperrors.LimitDiskFloor:                apperrors.ErrHostExhausted,
}

// Two ingestions race for the owner's last queued deployment. The first holds
// the owner capacity lock while it reads the reservation ledger; the second
// passed the early check beside it but waits for the lock, and once the first
// has inserted its job it counts that job and is refused.
func TestConcurrentDeploymentsCannotBothTakeTheLastQueuedDeployment(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := roomyPlanWorld()
		w.queuedDeployments = 0
		h := newPlanHarness(t, w, func(cfg *config.SystemConfig) { cfg.MaxQueuedComposeDeploysPerUser = 1 })
		gate := make(chan struct{})
		h.images.gate = gate

		errs := make(chan error, 2)
		deploy := func() {
			_, err := h.orchestrator.StartDeployment(h.ctx, "proj", "source.zip", "", bytes.NewReader(planArchive(t, planCompose)))
			errs <- err
		}
		go deploy()
		synctest.Wait()
		go deploy()
		synctest.Wait()
		require.Equal(t, 1, h.images.readCount(), "the second ingestion waits for the lock the first holds")

		close(gate)
		results := []error{<-errs, <-errs}

		admitted := 0
		for _, err := range results {
			if err == nil {
				admitted++
				continue
			}
			require.ErrorIs(t, err, apperrors.ErrLimitExceeded)
			require.Equal(t, apperrors.LimitQueuedComposeDeployments, apperrors.LimitOf(err))
		}
		require.Equal(t, 1, admitted, "one deployment is admitted")
		require.Equal(t, 1, h.deployments.count(), "one deployment is queued")
	})
}

// An owner who already has the allowed queued deployments is refused before
// the archive is uploaded: the object store and the plan admission are never
// called.
func TestStartDeploymentRefusesAFullQueueBeforeTheUpload(t *testing.T) {
	owner := uuid.New()
	deps := fullQueueDeps(t, owner)

	o, err := compose.NewOrchestrator(deps)
	require.NoError(t, err)
	_, err = o.StartDeployment(coretest.UserContext(owner), "proj", "source.zip", "", bytes.NewReader(planArchive(t, planCompose)))

	require.ErrorIs(t, err, apperrors.ErrLimitExceeded)
	require.Equal(t, apperrors.LimitQueuedComposeDeployments, apperrors.LimitOf(err))
}

// An owner who already has the allowed queued deployments is refused before
// the repository is cloned.
func TestStartGitDeploymentRefusesAFullQueueBeforeCloning(t *testing.T) {
	owner := uuid.New()
	deps := fullQueueDeps(t, owner)
	compose.SetCloneGitRepositoryForTest(t, func(context.Context, gitsource.CloneRequest) (gitsource.RepoInfo, error) {
		t.Fatal("a full queue must be refused before cloning")
		return gitsource.RepoInfo{}, nil
	})

	o, err := compose.NewOrchestrator(deps)
	require.NoError(t, err)
	_, err = o.StartGitDeployment(coretest.UserContext(owner), "proj", model.GitSource{RepoURL: "https://github.com/example/app.git"})

	require.ErrorIs(t, err, apperrors.ErrLimitExceeded)
	require.Equal(t, apperrors.LimitQueuedComposeDeployments, apperrors.LimitOf(err))
}

// fullQueueDeps are orchestrator dependencies for an owner holding both of
// their two queued deployments. The object store and the plan admission
// expect no call.
func fullQueueDeps(t *testing.T, owner uuid.UUID) compose.OrchestratorDeps {
	t.Helper()
	cfg := composeTestConfig()
	cfg.MaxQueuedComposeDeploysPerUser = 2
	counter := composemocks.NewActiveComposeDeploymentCounter(t)
	counter.EXPECT().CountActiveComposeDeploymentsByOwner(mock.Anything, owner).Return(2, nil).Once()
	return compose.OrchestratorDeps{
		Projects:          composemocks.NewProjectRepository(t),
		DeploymentCounter: counter,
		Capacity:          composemocks.NewCapacityChecker(t),
		Config:            coretest.StaticConfig{Config: cfg},
		ObjectStore:       composemocks.NewObjectStorage(t),
		Logger:            coretest.DiscardLogger(),
	}
}

// The plan's demand is one container per service with its own reservations
// or the defaults, its managed volumes by distinct name (prefixed with the
// project name) without the external ones, and one build per service that
// builds.
func TestStartDeploymentAsksForThePlansDemand(t *testing.T) {
	const reservingCompose = `
services:
  web:
    build:
      context: .
    volumes:
      - data:/data
      - shared:/shared
    deploy:
      resources:
        reservations:
          memory: 1g
          cpus: '1.5'
  worker:
    image: busybox:latest
    volumes:
      - data:/data
  cache:
    image: redis:latest
volumes:
  data:
  shared:
    external: true
`
	owner := uuid.New()
	cfg := composeTestConfig()
	cfg.ImageBuildsEnabled = true
	cfg.DefaultMemoryReservation = 256 << 20
	cfg.DefaultCPUReservation = 500
	repo := composemocks.NewProjectRepository(t)
	deps, _ := composeOrchestratorDeps(t, repo, cfg)
	capacity := composemocks.NewCapacityChecker(t)
	want := model.ComposePlanDemand{
		Services:           3,
		MemoryBytes:        1024<<20 + 2*256<<20,
		CPUMillicores:      1500 + 2*500,
		ManagedVolumeNames: []string{"proj_data"},
		Builds:             1,
	}
	capacity.EXPECT().AdmitPlan(mock.Anything, want, mock.Anything).Return(nil).Once()
	deps.Capacity = capacity
	o, err := compose.NewOrchestrator(deps)
	require.NoError(t, err)

	_, err = o.StartDeployment(coretest.UserContext(owner), "proj", "source.zip", "", bytes.NewReader(planArchive(t, reservingCompose)))

	require.NoError(t, err)
}

// planHarness is an orchestrator over the real plan admission, whose owner
// and host report a planWorld.
type planHarness struct {
	orchestrator *compose.Orchestrator
	deployments  *planDeployments
	images       *planImages
	objects      *coretest.MemoryObjects
	ctx          context.Context
}

func newPlanHarness(t *testing.T, w planWorld, adjust ...func(*config.SystemConfig)) *planHarness {
	t.Helper()
	owner := uuid.New()
	cfg := w.config()
	for _, a := range adjust {
		a(&cfg)
	}
	static := coretest.StaticConfig{Config: cfg}
	ctx := coretest.UserContext(owner)

	store := coretest.NewContainerStoreFake()
	running := []model.Container{
		{OwnerID: owner, BaseMemoryReservation: 512 * planMiB, BaseCPUReservation: 500, Status: model.ContainerStatusRunning},
		{OwnerID: uuid.New(), BaseMemoryReservation: w.otherMemory, BaseCPUReservation: w.otherCPU, Status: model.ContainerStatusRunning},
	}
	for range w.stoppedContainers {
		running = append(running, model.Container{OwnerID: owner, BaseMemoryReservation: 256 * planMiB, BaseCPUReservation: 250, Status: model.ContainerStatusExited})
	}
	for _, c := range running {
		c.ID = uuid.New()
		c.Name = "c-" + c.ID.String()[:8]
		c.DockerID = "docker-" + c.ID.String()[:12]
		require.NoError(t, store.Save(context.Background(), c))
	}

	quotas, err := userquota.New(coretest.NewUserQuotaStoreFake(), static)
	require.NoError(t, err)
	deployments := &planDeployments{queued: w.queuedDeployments}
	images := &planImages{bytes: w.usedDiskMB * planMiB}
	host := planHost{diskFreeBytes: w.diskFreeBytes}
	admission, err := service.NewComposePlanAdmission(service.ComposePlanAdmissionDeps{
		Containers:  store,
		Volumes:     planVolumes{owner: owner, names: w.volumeNames, unusedPromiseBytes: w.unusedPromiseBytes},
		Builds:      planBuilds{active: w.queuedBuilds},
		Images:      images,
		Deployments: deployments,
		Quotas:      quotas,
		HostMetrics: host,
		DiskMetrics: host,
		Config:      static,
		Logger:      coretest.DiscardLogger(),
	})
	require.NoError(t, err)

	repo := composemocks.NewProjectRepository(t)
	repo.EXPECT().CreateWithComposeDeploymentJob(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(context.Context, model.Project, model.ComposeDeploymentJob, model.ComposeDeploymentOutbox) error {
			deployments.add()
			return nil
		}).Maybe()
	objects := coretest.NewMemoryObjects()
	orchestrator, err := compose.NewOrchestrator(compose.OrchestratorDeps{
		Projects:          repo,
		DeploymentCounter: deployments,
		Capacity:          admission,
		Config:            static,
		ObjectStore:       objects,
		Logger:            coretest.DiscardLogger(),
	})
	require.NoError(t, err)
	return &planHarness{orchestrator: orchestrator, deployments: deployments, images: images, objects: objects, ctx: ctx}
}

// planDeployments counts the owner's queued deployments; an inserted job
// adds one.
type planDeployments struct {
	mu     sync.Mutex
	queued int
}

func (d *planDeployments) CountActiveComposeDeploymentsByOwner(context.Context, uuid.UUID) (int, error) {
	return d.count(), nil
}

func (d *planDeployments) add() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.queued++
}

func (d *planDeployments) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.queued
}

// planImages reports the owner's image disk. With a gate, the first read
// waits until the gate closes.
type planImages struct {
	mu    sync.Mutex
	bytes int64
	gate  chan struct{}
	reads int
}

func (i *planImages) GetUserImageBytes(context.Context, uuid.UUID) (int64, error) {
	i.mu.Lock()
	i.reads++
	first := i.reads == 1
	i.mu.Unlock()
	if first && i.gate != nil {
		<-i.gate
	}
	return i.bytes, nil
}

func (i *planImages) readCount() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.reads
}

// planVolumes holds the owner's volumes by name, each reserving nothing, and
// the host's unused volume promise.
type planVolumes struct {
	owner              uuid.UUID
	names              []string
	unusedPromiseBytes int64
}

func (v planVolumes) GetByName(_ context.Context, ownerID uuid.UUID, name string) (model.Volume, error) {
	if ownerID != v.owner || !slices.Contains(v.names, name) {
		return model.Volume{}, apperrors.ErrNotFound
	}
	return model.Volume{ID: uuid.New(), OwnerID: ownerID, Name: name, Status: model.VolumeStatusAvailable}, nil
}

func (v planVolumes) CountByOwnerID(context.Context, uuid.UUID) (int, error) {
	return len(v.names), nil
}

func (v planVolumes) GetUserReservedVolumeBytes(context.Context, uuid.UUID) (int64, error) {
	return 0, nil
}

func (v planVolumes) GetUnusedVolumePromiseBytes(context.Context) (int64, error) {
	return v.unusedPromiseBytes, nil
}

// planBuilds reports the owner's active builds, the only ones on the host.
type planBuilds struct{ active int }

func (b planBuilds) CountActive(context.Context) (int, error) { return b.active, nil }

func (b planBuilds) CountActiveByOwner(context.Context, uuid.UUID) (int, error) {
	return b.active, nil
}

// planHost is a 4 GiB, four-CPU host with diskFreeBytes free.
type planHost struct{ diskFreeBytes int64 }

func (planHost) GetTotalMemory() (int64, error) { return 4096 * planMiB, nil }
func (planHost) GetLogicalCPUs() (int64, error) { return 4, nil }
func (h planHost) GetDiskUsage(string) (model.HostDiskStats, error) {
	return model.HostDiskStats{FreeBytes: h.diskFreeBytes}, nil
}

// planArchive is a zip source holding composeYAML and a Dockerfile.
func planArchive(t *testing.T, composeYAML string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{"docker-compose.yml": composeYAML, "Dockerfile": "FROM busybox\n"} {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = io.WriteString(w, body)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}
