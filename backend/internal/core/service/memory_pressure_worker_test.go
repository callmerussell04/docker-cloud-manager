package service_test

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/userquota"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const pressureMiB = int64(1 << 20)

// pressureContainer is one running container the worker measures. Every one
// reserves 256 MiB.
type pressureContainer struct {
	name string
	// usedMiB is its working set; readErr makes the read fail instead.
	usedMiB int64
	readErr error
	// busy gives it a queued operation of its own before the check.
	busy bool
}

// The host has 4 GiB with 1 GiB held back for the system, so user containers
// share a 3 GiB pool, and the worker acts when less than 512 MiB of it is
// free.
func TestMemoryPressureWorkerStopsContainersOverTheirReservation(t *testing.T) {
	for _, tc := range []struct {
		name         string
		containers   []pressureContainer
		minFreeMiB   int64
		activeBuilds int
		// wantStopped names the containers a stop is queued for.
		wantStopped []string
	}{
		{
			name:       "enough free memory stops nothing",
			minFreeMiB: 512,
			containers: []pressureContainer{{name: "a", usedMiB: 1024}, {name: "b", usedMiB: 1024}},
		},
		{
			// 100 MiB is free: stopping the container furthest over its
			// reservation frees more than the 412 MiB shortfall.
			name:        "pressure stops the container furthest over its reservation",
			minFreeMiB:  512,
			containers:  []pressureContainer{{name: "small", usedMiB: 200}, {name: "over", usedMiB: 1172}, {name: "furthest", usedMiB: 1600}},
			wantStopped: []string{"furthest"},
		},
		{
			// 72 MiB is free against a 2 GiB floor: the two containers over
			// their reservation go, the one within it stays.
			name:        "a larger shortfall stops as many as it needs and none within its reservation",
			minFreeMiB:  2048,
			containers:  []pressureContainer{{name: "within", usedMiB: 256}, {name: "over", usedMiB: 1144}, {name: "furthest", usedMiB: 1600}},
			wantStopped: []string{"furthest", "over"},
		},
		{
			// Two build slots take 2 GiB of the pool, leaving 1 GiB that the
			// containers fill without any exceeding what it reserved.
			name:         "a short pool with every container within its reservation stops nothing",
			minFreeMiB:   512,
			activeBuilds: 2,
			containers:   []pressureContainer{{name: "a", usedMiB: 256}, {name: "b", usedMiB: 256}, {name: "c", usedMiB: 256}, {name: "d", usedMiB: 250}},
		},
		{
			name:         "build slots shrink the pool",
			minFreeMiB:   512,
			activeBuilds: 1,
			containers:   []pressureContainer{{name: "a", usedMiB: 900}, {name: "b", usedMiB: 1000}},
			wantStopped:  []string{"b"},
		},
		{
			name:        "a container with its own queued operation is passed over",
			minFreeMiB:  512,
			containers:  []pressureContainer{{name: "over", usedMiB: 1300}, {name: "furthest", usedMiB: 1600, busy: true}},
			wantStopped: []string{"over"},
		},
		{
			name:        "a container whose usage cannot be read is not stopped",
			minFreeMiB:  2048,
			containers:  []pressureContainer{{name: "unreadable", readErr: errors.New("stats unavailable")}, {name: "over", usedMiB: 1600}},
			wantStopped: []string{"over"},
		},
		{
			// With the floor at zero the worker reads nothing: the strict
			// Docker mock has no expectations.
			name:       "a zero floor turns the worker off",
			minFreeMiB: 0,
			containers: []pressureContainer{{name: "over", usedMiB: 3072, readErr: errNeverRead}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := coretest.NewContainerStoreFake()
			docker := coremocks.NewMemoryPressureDockerAPI(t)
			docker.EXPECT().ListRunningBuildContainers(mock.Anything).Return(nil, nil).Maybe()
			names := map[uuid.UUID]string{}
			for _, pc := range tc.containers {
				c := model.Container{
					ID:                    uuid.New(),
					OwnerID:               uuid.New(),
					Name:                  pc.name,
					DockerID:              "docker-" + pc.name,
					DockerGeneration:      1,
					Status:                model.ContainerStatusRunning,
					DesiredStatus:         model.ContainerStatusRunning,
					BaseMemoryReservation: 256 * pressureMiB,
				}
				require.NoError(t, store.Save(ctx, c))
				names[c.ID] = pc.name
				if pc.busy {
					op := model.ResourceOperation{ID: uuid.New(), ResourceType: model.ResourceTypeContainer, ResourceID: c.ID, OwnerID: c.OwnerID, Operation: model.OperationExpose, Status: model.OperationStatusPending}
					require.NoError(t, store.QueueContainerOperation(ctx, c.ID, model.ContainerStatusRunning, model.ContainerStatusRunning, op, model.ContainerLifecycleOutbox{ID: uuid.New(), OperationID: op.ID, ContainerID: c.ID, Status: model.ContainerOutboxStatusPublished}))
				}
				if errors.Is(pc.readErr, errNeverRead) {
					continue
				}
				docker.EXPECT().GetContainerUsage(mock.Anything, c.DockerID).Return(model.ContainerUsage{WorkingSetBytes: pc.usedMiB * pressureMiB, MemoryLimitBytes: 1024 * pressureMiB}, pc.readErr).Once()
			}
			cfg := testSystemConfig()
			cfg.DefaultMemoryReservation = 256 * pressureMiB
			cfg.ReservedSystemMemory = 1024 * pressureMiB
			cfg.BuildMemoryBytes = 1024 * pressureMiB
			cfg.MaxConcurrentBuilds = 2
			cfg.MemoryPressureMinFreeBytes = tc.minFreeMiB * pressureMiB
			metrics := coremocks.NewMemoryPressureHostMetrics(t)
			metrics.EXPECT().GetTotalMemory().Return(4096*pressureMiB, nil).Maybe()
			builds := coremocks.NewActiveSystemBuildCounter(t)
			builds.EXPECT().CountActive(mock.Anything).Return(tc.activeBuilds, nil).Maybe()
			worker, err := service.NewMemoryPressureWorker(service.MemoryPressureWorkerDeps{
				Repository: store,
				Docker:     docker,
				Metrics:    metrics,
				Builds:     builds,
				// Only containers run, so no build is stopped.
				BuildStopper: coremocks.NewMemoryPressureBuildStopper(t),
				Quotas:       testQuotas(t, nil, nil),
				Config:       coretest.StaticConfig{Config: cfg},
				Logger:       slog.New(slog.DiscardHandler),
			})
			require.NoError(t, err)

			worker.RelievePressure(ctx)

			outbox, err := store.LeasePendingContainerOutbox(ctx, 10)
			require.NoError(t, err)
			var stopped []string
			for _, record := range outbox {
				var msg queue.ContainerLifecycleMessage
				require.NoError(t, json.Unmarshal(record.Payload, &msg))
				require.Equal(t, model.OperationStop, msg.Operation)
				require.Equal(t, model.ContainerStatusRunning, msg.PreviousStatus)
				require.Equal(t, service.MemoryPressureStopReason, msg.Reason, "the stop explains itself to the owner")
				c, err := store.GetByID(ctx, record.ContainerID)
				require.NoError(t, err)
				require.Equal(t, model.ContainerStatusStopping, c.Status)
				require.Equal(t, model.ContainerStatusExited, c.DesiredStatus, "a stopped container gives its reservation back")
				stopped = append(stopped, names[record.ContainerID])
			}
			require.Equal(t, tc.wantStopped, stopped, "stops are queued largest excess first")
		})
	}
}

// errNeverRead marks a container the worker must not measure.
var errNeverRead = errors.New("usage must not be read")

func TestNewMemoryPressureWorkerRejectsMissingDependencies(t *testing.T) {
	valid := func(t *testing.T) service.MemoryPressureWorkerDeps {
		return service.MemoryPressureWorkerDeps{
			Repository:   coretest.NewContainerStoreFake(),
			Docker:       coremocks.NewMemoryPressureDockerAPI(t),
			Metrics:      coremocks.NewMemoryPressureHostMetrics(t),
			Builds:       coremocks.NewActiveSystemBuildCounter(t),
			BuildStopper: coremocks.NewMemoryPressureBuildStopper(t),
			Quotas:       testQuotas(t, nil, nil),
			Config:       testConfig(),
		}
	}
	depstest.RequireRejectsMissing(t, valid, service.NewMemoryPressureWorker, map[string]string{
		"Repository":   "repository",
		"Docker":       "docker",
		"Metrics":      "metrics",
		"Builds":       "builds",
		"BuildStopper": "build stopper",
		"Quotas":       "quotas",
		"Config":       "config",
	})
}

// boostContainer is one running container in a boost pass. Unless set, it
// reserves 256 MiB, so its standard limit is 1 GiB and its boost ceiling
// 2 GiB, and it runs at its standard limit.
type boostContainer struct {
	name           string
	reservationMiB int64
	usedMiB        int64
	limitMiB       int64
	// owner names a user shared with other containers; empty gives the
	// container a user of its own.
	owner     string
	busy      bool
	updateErr error
}

func (bc boostContainer) reservation() int64 { return cmp.Or(bc.reservationMiB, 256) * pressureMiB }

func (bc boostContainer) limit() int64 {
	return cmp.Or(bc.limitMiB*pressureMiB, bc.reservation()*boostBurstMultiplier)
}

const boostBurstMultiplier = 4

// stopOf is a queued stop: the container and the reason recorded for it.
type stopOf struct{ name, reason string }

// boostBuild is one running build container in a boost pass. Its
// reservation and standard limit are the 1 GiB build memory, and unless set
// it runs at that limit.
type boostBuild struct {
	name     string
	usedMiB  int64
	limitMiB int64
	owner    string
	// stopErr fails the build's stop, as when its status cannot be
	// recorded.
	stopErr error
}

func (bb boostBuild) limit() int64 { return cmp.Or(bb.limitMiB, 1024) * pressureMiB }

// The host has 8 GiB with 1 GiB held back for the system, so user containers
// share a 7 GiB pool, less 1 GiB for each running build. Boosts are granted
// while at least 2 GiB of it stays free, and containers are stopped when less
// than 512 MiB is free. A limit grows by half its standard limit per pass. A
// build uses the pool only with what it uses above its 1 GiB slot.
func TestMemoryPressureWorkerBoostsAndReclaimsMemory(t *testing.T) {
	for _, tc := range []struct {
		name       string
		containers []boostContainer
		builds     []boostBuild
		// boostMultiplier defaults to 8.
		boostMultiplier int64
		// quotaMiB is the custom memory quota of a named owner; others have
		// the default quota, 64 GiB unless defaultQuotaMiB sets it.
		quotaMiB        map[string]int64
		defaultQuotaMiB int64
		// quotaUnreadable makes every quota read fail.
		quotaUnreadable bool
		// wantLimitsMiB is every limit update sent to Docker.
		wantLimitsMiB map[string]int64
		wantStopped   []stopOf
		// wantBuildsStopped names the builds stopped through the build
		// service, which records them failed_resource_exhausted and revokes
		// them with tag deletion (TestBuildServiceStopBuildForResourceExhaustion),
		// in order. Docker gets no kill or stop for a build: its mock has none.
		wantBuildsStopped []string
	}{
		{
			name:          "an idle host raises a container near its limit by one step and leaves the others alone",
			containers:    []boostContainer{{name: "near", usedMiB: 900}, {name: "calm", usedMiB: 500}},
			wantLimitsMiB: map[string]int64{"near": 1536},
		},
		{
			name:          "a raise stops at the boost ceiling",
			containers:    []boostContainer{{name: "near", usedMiB: 1800, limitMiB: 1900}},
			wantLimitsMiB: map[string]int64{"near": 2048},
		},
		{
			name:          "a container at its boost ceiling is not raised",
			containers:    []boostContainer{{name: "full", usedMiB: 2000, limitMiB: 2048}},
			wantLimitsMiB: map[string]int64{},
		},
		{
			// 5056 MiB is used, so 2112 MiB is free and only 64 MiB of it
			// may be lent.
			name:          "a grant never takes free memory below the boost floor",
			containers:    []boostContainer{{name: "near", usedMiB: 900}, {name: "filler", reservationMiB: 2048, usedMiB: 4156}},
			wantLimitsMiB: map[string]int64{"near": 1088},
		},
		{
			name:          "a grant below 16 MiB is not made",
			containers:    []boostContainer{{name: "near", usedMiB: 900}, {name: "filler", reservationMiB: 2048, usedMiB: 4210}},
			wantLimitsMiB: map[string]int64{},
		},
		{
			// 300 MiB may be lent and the two want 512 and 1024 MiB: the
			// one reserving twice as much gets twice as much.
			name: "competing candidates split the spare memory by reservation",
			containers: []boostContainer{
				{name: "small", usedMiB: 900},
				{name: "large", reservationMiB: 512, usedMiB: 1800},
				{name: "filler", reservationMiB: 1024, usedMiB: 2120},
			},
			wantLimitsMiB: map[string]int64{"small": 1124, "large": 2248},
		},
		{
			// The owner already borrows 512 MiB of a 768 MiB quota.
			name: "a user's borrowed memory stays within their memory quota",
			containers: []boostContainer{
				{name: "boosted", owner: "u", usedMiB: 600, limitMiB: 1536},
				{name: "near", owner: "u", usedMiB: 900},
			},
			quotaMiB:      map[string]int64{"u": 768},
			wantLimitsMiB: map[string]int64{"near": 1280},
		},
		{
			name: "a user on a 768 MiB default quota borrows within it",
			containers: []boostContainer{
				{name: "boosted", owner: "u", usedMiB: 600, limitMiB: 1536},
				{name: "near", owner: "u", usedMiB: 900},
			},
			defaultQuotaMiB: 768,
			wantLimitsMiB:   map[string]int64{"near": 1280},
		},
		{
			name: "a custom quota lets a user borrow more than the default",
			containers: []boostContainer{
				{name: "boosted", owner: "u", usedMiB: 600, limitMiB: 1536},
				{name: "near", owner: "u", usedMiB: 900},
			},
			defaultQuotaMiB: 768,
			quotaMiB:        map[string]int64{"u": 4096},
			wantLimitsMiB:   map[string]int64{"near": 1536},
		},
		{
			name:            "an owner whose quota cannot be read is lent nothing",
			containers:      []boostContainer{{name: "near", usedMiB: 900}},
			quotaUnreadable: true,
			wantLimitsMiB:   map[string]int64{},
		},
		{
			// 6060 MiB is used, leaving 1108 MiB free: below the boost
			// floor, above the stop floor. The small container keeps at
			// least 64 MiB above its usage.
			name: "a busy host takes back unused headroom, never below usage plus the margin or the standard limit",
			containers: []boostContainer{
				{name: "margin", usedMiB: 1200, limitMiB: 2048},
				{name: "standard", usedMiB: 500, limitMiB: 1536},
				{name: "close", usedMiB: 1260, limitMiB: 1400},
				{name: "small", reservationMiB: 128, usedMiB: 600, limitMiB: 1024},
				{name: "filler", reservationMiB: 2048, usedMiB: 2500},
			},
			wantLimitsMiB: map[string]int64{"margin": 1320, "standard": 1024, "small": 664},
		},
		{
			// 118 MiB is free. The borrower is stopped although the large
			// container is further over its reservation; the idle borrower
			// only gives back headroom.
			name: "under pressure a container using borrowed memory is stopped first",
			containers: []boostContainer{
				{name: "borrower", usedMiB: 1900, limitMiB: 2048},
				{name: "large", reservationMiB: 2048, usedMiB: 4600},
				{name: "within", usedMiB: 250},
				{name: "idle", usedMiB: 300, limitMiB: 1536},
			},
			wantLimitsMiB: map[string]int64{"idle": 1024},
			wantStopped:   []stopOf{{"borrower", service.BorrowedMemoryStopReason}},
		},
		{
			name: "a larger shortfall then stops containers over their reservation and never one within it",
			containers: []boostContainer{
				{name: "borrower", usedMiB: 1100, limitMiB: 2048},
				{name: "large", reservationMiB: 2048, usedMiB: 6500},
				{name: "within", usedMiB: 256},
			},
			wantLimitsMiB: map[string]int64{},
			wantStopped:   []stopOf{{"borrower", service.BorrowedMemoryStopReason}, {"large", service.MemoryPressureStopReason}},
		},
		{
			name:          "a container with its own queued operation is skipped",
			containers:    []boostContainer{{name: "busy", usedMiB: 900, busy: true}, {name: "near", usedMiB: 900}},
			wantLimitsMiB: map[string]int64{"near": 1536},
		},
		{
			name: "a failed update does not stop the pass",
			containers: []boostContainer{
				{name: "broken", usedMiB: 900, updateErr: errors.New("update rejected")},
				{name: "near", usedMiB: 900},
			},
			wantLimitsMiB: map[string]int64{"broken": 1536, "near": 1536},
		},
		{
			// Even on an idle host: the limit raised earlier comes back as
			// far as its usage plus the margin allows.
			name:            "with boost off nothing is raised and a limit raised earlier returns toward standard",
			boostMultiplier: boostBurstMultiplier,
			containers: []boostContainer{
				{name: "near", usedMiB: 900},
				{name: "boosted", usedMiB: 300, limitMiB: 2048},
				{name: "using", usedMiB: 1500, limitMiB: 2048},
			},
			wantLimitsMiB: map[string]int64{"boosted": 1024, "using": 1650},
		},
		{
			// The ceiling is now 1280 MiB. The host is idle, yet limits
			// above the ceiling come down to it or to usage plus the
			// margin, and a pass that lowers a limit raises none.
			name:            "a lowered boost ceiling takes back the headroom above it",
			boostMultiplier: 5,
			containers: []boostContainer{
				{name: "over", usedMiB: 600, limitMiB: 2048},
				{name: "using", usedMiB: 1500, limitMiB: 2048},
				{name: "within", usedMiB: 600, limitMiB: 1200},
				{name: "near", usedMiB: 900},
			},
			wantLimitsMiB: map[string]int64{"over": 1280, "using": 1650},
		},
		{
			// Boost was turned off while the build ran on a raised limit.
			// It still takes 576 MiB from the pool, leaving 220 MiB free,
			// and is the furthest over its reservation, so it goes first.
			name:            "with boost off a build left above its build memory is still counted and stopped like a container over its reservation",
			boostMultiplier: boostBurstMultiplier,
			containers: []boostContainer{
				{name: "filler", reservationMiB: 2560, usedMiB: 2500},
				{name: "filler2", reservationMiB: 2048, usedMiB: 2048},
				{name: "over", usedMiB: 800},
			},
			builds:            []boostBuild{{name: "leftover", usedMiB: 1600, limitMiB: 2048}},
			wantLimitsMiB:     map[string]int64{},
			wantBuildsStopped: []string{"leftover"},
		},
		{
			name:            "with boost off a build left on a raised limit returns to its build memory",
			boostMultiplier: boostBurstMultiplier,
			builds:          []boostBuild{{name: "leftover", usedMiB: 600, limitMiB: 2048}},
			wantLimitsMiB:   map[string]int64{"leftover": 1024},
		},
		{
			name:          "an idle host raises a build near its limit by one step",
			containers:    []boostContainer{{name: "calm", usedMiB: 500}},
			builds:        []boostBuild{{name: "build", usedMiB: 900}, {name: "quiet", usedMiB: 400}},
			wantLimitsMiB: map[string]int64{"build": 1536},
		},
		{
			// The ceiling is 5 GiB. The build uses 3576 MiB above its slot,
			// leaving 2568 MiB free, so 520 MiB could be lent: the ceiling
			// binds first.
			name:            "a build raise stops at its boost ceiling",
			boostMultiplier: 5,
			builds:          []boostBuild{{name: "build", usedMiB: 4600, limitMiB: 5000}},
			wantLimitsMiB:   map[string]int64{"build": 5120},
		},
		{
			// Two builds leave a 5 GiB pool; 1944 MiB of it is free.
			name:          "a busy host lowers a boosted build, never below usage plus the margin or its build memory",
			containers:    []boostContainer{{name: "filler", reservationMiB: 2048, usedMiB: 2700}},
			builds:        []boostBuild{{name: "margin", usedMiB: 1500, limitMiB: 2560}, {name: "standard", usedMiB: 600, limitMiB: 1536}},
			wantLimitsMiB: map[string]int64{"margin": 1650, "standard": 1024},
		},
		{
			// 756 MiB is missing below zero, a 1268 MiB shortfall. Stopping
			// the build frees the 976 MiB it uses above its slot, stopping
			// the container frees the rest; the container over its
			// reservation without borrowing stays, and the build within its
			// build memory only gives back headroom.
			name: "under pressure a build using borrowed memory is stopped largest borrower first",
			containers: []boostContainer{
				{name: "borrower", usedMiB: 1600, limitMiB: 2048},
				{name: "large", reservationMiB: 2048, usedMiB: 3300},
			},
			builds:            []boostBuild{{name: "build", usedMiB: 2000, limitMiB: 3072}, {name: "within", usedMiB: 1000, limitMiB: 1536}},
			wantLimitsMiB:     map[string]int64{"within": 1100},
			wantStopped:       []stopOf{{"borrower", service.BorrowedMemoryStopReason}},
			wantBuildsStopped: []string{"build"},
		},
		{
			// The same pool as above, but the build's stop cannot be
			// recorded: it is passed over and the pass continues, and
			// stopping the container that borrows next covers the whole
			// shortfall on its own.
			name: "a build whose stop cannot be recorded is passed over and the pass continues",
			containers: []boostContainer{
				{name: "borrower", usedMiB: 1600, limitMiB: 2048},
				{name: "large", reservationMiB: 2048, usedMiB: 3300},
			},
			builds: []boostBuild{{name: "build", usedMiB: 2000, limitMiB: 3072, stopErr: errors.New("database unavailable")}, {name: "within", usedMiB: 1000, limitMiB: 1536}},
			// The build still running is lowered toward its use like any
			// workload that was not stopped.
			wantLimitsMiB: map[string]int64{"build": 2200, "within": 1100},
			wantStopped:   []stopOf{{"borrower", service.BorrowedMemoryStopReason}},
		},
		{
			// Two build slots and the filler leave 420 MiB of the pool free,
			// below the stop floor, but nothing uses more than it reserved.
			name:          "under pressure a build within its build memory is never stopped",
			containers:    []boostContainer{{name: "filler", reservationMiB: 4700, usedMiB: 4700}},
			builds:        []boostBuild{{name: "full", usedMiB: 1024}, {name: "near", usedMiB: 1000}},
			wantLimitsMiB: map[string]int64{},
		},
		{
			// A 744 MiB shortfall: the container borrows more than the
			// build, so stopping it alone covers the shortfall.
			name: "a container borrowing more than a build is stopped before the build",
			containers: []boostContainer{
				{name: "borrower", usedMiB: 2000, limitMiB: 2048},
				{name: "filler", reservationMiB: 2048, usedMiB: 4000},
			},
			builds:        []boostBuild{{name: "build", usedMiB: 1400, limitMiB: 2048}},
			wantLimitsMiB: map[string]int64{"build": 1540},
			wantStopped:   []stopOf{{"borrower", service.BorrowedMemoryStopReason}},
		},
		{
			// The owner's container already borrows 512 MiB of a 768 MiB
			// quota.
			name:          "a build's borrowed memory counts toward its owner's quota",
			containers:    []boostContainer{{name: "boosted", owner: "u", usedMiB: 600, limitMiB: 1536}},
			builds:        []boostBuild{{name: "build", owner: "u", usedMiB: 900}},
			quotaMiB:      map[string]int64{"u": 768},
			wantLimitsMiB: map[string]int64{"build": 1280},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := boostTestConfig()
			if tc.boostMultiplier != 0 {
				cfg.MemoryBoostMultiplier = tc.boostMultiplier
			}
			if tc.defaultQuotaMiB != 0 {
				cfg.DefaultUserQuotaRAMMB = tc.defaultQuotaMiB
			}
			pass := newBoostPass(t, cfg, tc.quotaMiB)
			if tc.quotaUnreadable {
				pass.quotas.SetFaults(coretest.UserQuotaStoreFaults{GetCustomUserQuota: coretest.FailAlways(errors.New("quota store unavailable"))})
			}
			for _, bc := range tc.containers {
				pass.add(bc)
			}
			for _, bb := range tc.builds {
				pass.addBuild(bb)
			}

			pass.worker.RelievePressure(ctx)

			require.Equal(t, tc.wantLimitsMiB, pass.updatedMiB(), "limit updates sent to Docker")
			require.Equal(t, tc.wantStopped, pass.stopped(), "stops queued, in order")
			require.Equal(t, tc.wantBuildsStopped, pass.buildsStopped, "builds stopped through the build service, in order")
		})
	}
}

// A limit lowered under pressure is not raised again for six passes, so it
// does not swing while the host hovers around the boost floor.
func TestMemoryPressureWorkerDoesNotRaiseALoweredLimitWithinTheCooldown(t *testing.T) {
	ctx := context.Background()
	pass := newBoostPass(t, boostTestConfig(), nil)
	pass.add(boostContainer{name: "borrower", usedMiB: 1000, limitMiB: 2048})
	// The first pass sees a busy host; the rest see the filler gone.
	pass.add(boostContainer{name: "filler", reservationMiB: 2048, usedMiB: 5000})

	var history []int64
	for passes := 1; passes <= 8; passes++ {
		if passes == 2 {
			pass.usedMiB["filler"] = 10
		}
		pass.worker.RelievePressure(ctx)
		history = append(history, pass.limits["borrower"]/pressureMiB)
	}

	require.Equal(t, []int64{1100, 1100, 1100, 1100, 1100, 1100, 1100, 1612}, history,
		"lowered to usage plus 10%, held for six passes, then raised one step")
}

// Monitoring shows how much memory runs on boost: the sum, over the running
// workloads the last pass measured, of their limits above standard after it
// applied its changes. It is 0 before the first pass and while memory boost
// is off.
func TestMemoryPressureWorkerReportsTheMemoryBorrowedAtItsLastPass(t *testing.T) {
	ctx := context.Background()
	cfg := boostTestConfig()
	pass := newBoostPass(t, cfg, nil)
	pass.add(boostContainer{name: "boosted", usedMiB: 600, limitMiB: 1536})
	pass.add(boostContainer{name: "standard", usedMiB: 500})
	pass.add(boostContainer{name: "near", usedMiB: 900})
	pass.addBuild(boostBuild{name: "build", usedMiB: 900, limitMiB: 1280})
	require.Zero(t, pass.worker.BorrowedMemoryBytes(), "nothing measured yet")

	pass.worker.RelievePressure(ctx)
	require.Equal(t, map[string]int64{"near": 1536}, pass.updatedMiB())
	require.Equal(t, (512+256+512)*pressureMiB, pass.worker.BorrowedMemoryBytes(), "including the raise this pass made")

	off := newBoostPass(t, func() config.SystemConfig { c := cfg; c.MemoryBoostMultiplier = boostBurstMultiplier; return c }(), nil)
	off.add(boostContainer{name: "boosted", usedMiB: 1200, limitMiB: 1536})
	off.worker.RelievePressure(ctx)
	require.Equal(t, 296*pressureMiB, off.worker.BorrowedMemoryBytes(),
		"with memory boost off, what a limit raised earlier still holds above standard after it was lowered to usage plus the margin")
}

// cpuWorkload is a running container, or a build, in a CPU boost test. It
// uses 100 MiB of memory, far from its memory limit.
type cpuWorkload struct {
	name  string
	build bool
	// usedMillicores is its CPU use between the two passes.
	usedMillicores int64
	// quota is its CPU quota now; unless set it is its standard quota.
	quota int64
	// period is the CPU period Docker reports; unless set it is the
	// configured one.
	period int64
	busy   bool
}

// oneCore is the CPU quota of one core over the 100000 microsecond period.
const oneCore = int64(100000)

// cpuPassGap is the time between the two passes of a CPU boost test.
const cpuPassGap = 10 * time.Second

// Every container reserves 250 millicores, so its standard CPU quota is one
// core and its CPU boost ceiling two; a build's standard quota is one core
// and its ceiling eight. The worker decides on CPU from what a workload used
// between two passes, so the first pass changes nothing.
func TestMemoryPressureWorkerBoostsCPUWhileTheHostIsIdle(t *testing.T) {
	for _, tc := range []struct {
		name      string
		hostLoad  float64
		workloads []cpuWorkload
		// cpuBoostMultiplier defaults to 8.
		cpuBoostMultiplier int64
		// wantQuotas is every CPU quota sent to Docker, and wantPeriods
		// every CPU period sent with one.
		wantQuotas    map[string]int64
		wantPeriods   map[string]int64
		wantInspected []string
		// wantLoadReads is how often the second pass reads host CPU load.
		wantLoadReads int
	}{
		{
			name:          "an idle host raises a container near its CPU quota by one standard quota and leaves the others alone",
			hostLoad:      30,
			workloads:     []cpuWorkload{{name: "busy", usedMillicores: 950}, {name: "calm", usedMillicores: 300}},
			wantQuotas:    map[string]int64{"busy": 2 * oneCore},
			wantInspected: []string{"busy"},
			wantLoadReads: 1,
		},
		{
			name:          "a raise stops at the CPU boost ceiling",
			hostLoad:      30,
			workloads:     []cpuWorkload{{name: "boosted", usedMillicores: 1400, quota: 3 * oneCore / 2}},
			wantQuotas:    map[string]int64{"boosted": 2 * oneCore},
			wantInspected: []string{"boosted"},
			wantLoadReads: 1,
		},
		{
			name:          "a container at its CPU boost ceiling is not raised",
			hostLoad:      30,
			workloads:     []cpuWorkload{{name: "full", usedMillicores: 1900, quota: 2 * oneCore}},
			wantQuotas:    map[string]int64{},
			wantInspected: []string{"full"},
			wantLoadReads: 1,
		},
		{
			// The ceiling is four cores; a core used of two is not near.
			name:               "a raised quota is not raised again until it is used",
			hostLoad:           30,
			cpuBoostMultiplier: 16,
			workloads:          []cpuWorkload{{name: "boosted", usedMillicores: 1000, quota: 2 * oneCore}},
			wantQuotas:         map[string]int64{},
			wantInspected:      []string{"boosted"},
			wantLoadReads:      1,
		},
		{
			name:          "a busy host returns a raised quota to standard",
			hostLoad:      90,
			workloads:     []cpuWorkload{{name: "boosted", usedMillicores: 1500, quota: 2 * oneCore}, {name: "standard", usedMillicores: 950}},
			wantQuotas:    map[string]int64{"boosted": oneCore},
			wantInspected: []string{"boosted", "standard"},
			wantLoadReads: 1,
		},
		{
			// Two cores over a 50000 period, set before the period setting
			// changed: the standard quota goes back with today's period.
			name:          "a busy host returns a quota set under another CPU period to standard",
			hostLoad:      90,
			workloads:     []cpuWorkload{{name: "boosted", usedMillicores: 1500, quota: oneCore, period: oneCore / 2}},
			wantQuotas:    map[string]int64{"boosted": oneCore},
			wantPeriods:   map[string]int64{"boosted": oneCore},
			wantInspected: []string{"boosted"},
			wantLoadReads: 1,
		},
		{
			name:          "an idle host does not raise a quota set under another CPU period",
			hostLoad:      30,
			workloads:     []cpuWorkload{{name: "busy", usedMillicores: 950, quota: oneCore / 2, period: oneCore / 2}},
			wantQuotas:    map[string]int64{},
			wantInspected: []string{"busy"},
			wantLoadReads: 1,
		},
		{
			name:          "a host neither idle nor busy is left alone without inspecting anything",
			hostLoad:      70,
			workloads:     []cpuWorkload{{name: "busy", usedMillicores: 950}, {name: "boosted", usedMillicores: 1500, quota: 2 * oneCore}},
			wantQuotas:    map[string]int64{},
			wantLoadReads: 1,
		},
		{
			name:          "host load is not read when no workload is near its standard CPU quota",
			hostLoad:      30,
			workloads:     []cpuWorkload{{name: "calm", usedMillicores: 300}},
			wantQuotas:    map[string]int64{},
			wantLoadReads: 0,
		},
		{
			name:               "a CPU boost multiplier at the CPU burst multiplier reads, inspects and changes nothing",
			hostLoad:           30,
			cpuBoostMultiplier: 4,
			workloads:          []cpuWorkload{{name: "busy", usedMillicores: 950}, {name: "boosted", usedMillicores: 1500, quota: 2 * oneCore}},
			wantQuotas:         map[string]int64{},
			wantLoadReads:      0,
		},
		{
			name:          "an idle host raises a build near its CPU quota",
			hostLoad:      30,
			workloads:     []cpuWorkload{{name: "build", build: true, usedMillicores: 950}},
			wantQuotas:    map[string]int64{"build": 2 * oneCore},
			wantInspected: []string{"build"},
			wantLoadReads: 1,
		},
		{
			name:          "a container with its own queued operation keeps its CPU quota",
			hostLoad:      30,
			workloads:     []cpuWorkload{{name: "queued", usedMillicores: 950, busy: true}, {name: "busy", usedMillicores: 950}},
			wantQuotas:    map[string]int64{"busy": 2 * oneCore},
			wantInspected: []string{"queued", "busy"},
			wantLoadReads: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx := context.Background()
				cfg := boostTestConfig()
				cfg.CPUBoostMultiplier = cmp.Or(tc.cpuBoostMultiplier, 8)
				pass := newBoostPass(t, cfg, nil)
				loads := 0
				pass.metrics.EXPECT().GetCPULoad().RunAndReturn(func() (float64, error) {
					loads++
					return tc.hostLoad, nil
				}).Maybe()
				for _, wl := range tc.workloads {
					if wl.build {
						pass.addBuild(boostBuild{name: wl.name, usedMiB: 100})
					} else {
						pass.add(boostContainer{name: wl.name, usedMiB: 100, busy: wl.busy})
					}
					pass.cpuQuota[wl.name] = cmp.Or(wl.quota, oneCore)
					pass.cpuPeriod[wl.name] = cmp.Or(wl.period, oneCore)
				}

				pass.worker.RelievePressure(ctx)
				require.Empty(t, pass.cpuSent, "nothing is decided on a first sample")
				require.Zero(t, loads, "nothing to decide, so host load is not read")
				time.Sleep(cpuPassGap)
				for _, wl := range tc.workloads {
					pass.cpuNanos[wl.name] += cpuNanosecondsOver(wl.usedMillicores, cpuPassGap)
				}
				pass.worker.RelievePressure(ctx)

				require.Equal(t, tc.wantQuotas, pass.updatedQuotas(), "CPU quotas sent to Docker")
				wantPeriods := tc.wantPeriods
				if wantPeriods == nil {
					wantPeriods = map[string]int64{}
				}
				require.Equal(t, wantPeriods, pass.periodSent, "CPU periods sent to Docker")
				require.ElementsMatch(t, tc.wantInspected, pass.inspected, "workloads inspected")
				require.Equal(t, tc.wantLoadReads, loads, "host CPU load reads")
				require.Empty(t, pass.sent, "CPU boost changes no memory limit")
			})
		})
	}
}

// A memory and a CPU change for one container in one pass go to Docker as
// one update.
func TestMemoryPressureWorkerSendsOneUpdatePerWorkloadAndPass(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		cfg := boostTestConfig()
		cfg.CPUBoostMultiplier = 8
		pass := newBoostPass(t, cfg, nil)
		pass.metrics.EXPECT().GetCPULoad().Return(30, nil).Maybe()
		pass.add(boostContainer{name: "both", usedMiB: 500})
		pass.cpuQuota["both"] = oneCore

		pass.worker.RelievePressure(ctx)
		time.Sleep(cpuPassGap)
		pass.usedMiB["both"] = 900
		pass.cpuNanos["both"] += cpuNanosecondsOver(950, cpuPassGap)
		pass.worker.RelievePressure(ctx)

		require.Equal(t, 1, pass.updates["both"], "one Docker update")
		require.Equal(t, map[string]int64{"both": 1536}, pass.updatedMiB())
		require.Equal(t, map[string]int64{"both": 2 * oneCore}, pass.updatedQuotas())
	})
}

// A quota returned to standard on a busy host is not raised again for six
// passes, so a workload whose own use makes the host busy does not swing
// between boosted and throttled.
func TestMemoryPressureWorkerDoesNotRaiseAReturnedCPUQuotaWithinTheCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		cfg := boostTestConfig()
		cfg.CPUBoostMultiplier = 8
		pass := newBoostPass(t, cfg, nil)
		hostLoad := 90.0
		pass.metrics.EXPECT().GetCPULoad().RunAndReturn(func() (float64, error) { return hostLoad, nil }).Maybe()
		pass.add(boostContainer{name: "boosted", usedMiB: 100})
		pass.cpuQuota["boosted"] = 2 * oneCore

		pass.worker.RelievePressure(ctx)
		var history []int64
		for passes := 1; passes <= 8; passes++ {
			if passes == 2 {
				hostLoad = 30
			}
			time.Sleep(cpuPassGap)
			pass.cpuNanos["boosted"] += cpuNanosecondsOver(950, cpuPassGap)
			pass.worker.RelievePressure(ctx)
			history = append(history, pass.cpuQuota["boosted"]/oneCore)
		}

		require.Equal(t, []int64{1, 1, 1, 1, 1, 1, 1, 2}, history,
			"returned to standard, held for six passes on an idle host, then raised one step")
	})
}

// A memory limit is lowered before the pass waits on host CPU load, so it
// rests on a fresh working set, and that workload gets no CPU change in the
// same pass.
func TestMemoryPressureWorkerLowersLimitsBeforeReadingHostCPULoad(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		cfg := boostTestConfig()
		cfg.CPUBoostMultiplier = 8
		pass := newBoostPass(t, cfg, nil)
		var limitAtLoadRead int64
		pass.metrics.EXPECT().GetCPULoad().RunAndReturn(func() (float64, error) {
			limitAtLoadRead = pass.limits["boosted"] / pressureMiB
			return 30, nil
		}).Once()
		pass.add(boostContainer{name: "boosted", usedMiB: 1000, limitMiB: 2048})
		pass.cpuQuota["boosted"] = oneCore
		pass.add(boostContainer{name: "busy", usedMiB: 100})
		pass.cpuQuota["busy"] = oneCore
		pass.add(boostContainer{name: "filler", reservationMiB: 2048, usedMiB: 10})

		pass.worker.RelievePressure(ctx)
		time.Sleep(cpuPassGap)
		pass.usedMiB["filler"] = 5000
		pass.cpuNanos["boosted"] += cpuNanosecondsOver(950, cpuPassGap)
		pass.cpuNanos["busy"] += cpuNanosecondsOver(950, cpuPassGap)
		pass.worker.RelievePressure(ctx)

		require.EqualValues(t, 1100, limitAtLoadRead, "already lowered when host load is read")
		require.Equal(t, 1, pass.updates["boosted"], "one Docker update")
		require.Equal(t, map[string]int64{"busy": 2 * oneCore}, pass.updatedQuotas(), "the lowered workload waits a pass for CPU")
	})
}

func cpuNanosecondsOver(millicores int64, d time.Duration) int64 {
	return millicores * d.Nanoseconds() / 1000
}

func boostTestConfig() config.SystemConfig {
	cfg := testSystemConfig()
	cfg.DefaultMemoryReservation = 256 * pressureMiB
	cfg.MaxBurstMultiplier = boostBurstMultiplier
	cfg.ContainerMemorySwapMultiplier = 2
	cfg.ReservedSystemMemory = 1024 * pressureMiB
	cfg.MemoryPressureMinFreeBytes = 512 * pressureMiB
	cfg.MemoryBoostMultiplier = 8
	cfg.BoostMinFreeMemoryBytes = 2048 * pressureMiB
	cfg.BuildMemoryBytes = 1024 * pressureMiB
	cfg.BuildMemorySwapMultiplier = 3
	cfg.MaxConcurrentBuilds = 2
	// Owners without a custom quota may borrow up to 64 GiB.
	cfg.DefaultUserQuotaRAMMB = 64 * 1024
	cfg.DefaultCPUReservation = 250
	cfg.MaxCPUBurstMultiplier = 4
	cfg.ContainerCPUPeriod = oneCore
	cfg.BuildCPUMillicores = 1000
	cfg.BuildCPUPeriod = oneCore
	// CPU boost is off unless a test turns it on.
	cfg.CPUBoostMultiplier = 1
	return cfg
}

// boostPass is a memory pressure worker over the container store fake and a
// mocked Docker. Docker reports the usage the test scripts and the limits
// and quotas it was last sent, and records the updates and inspections it
// gets. The build stopper records the builds the worker stops.
type boostPass struct {
	t       *testing.T
	cfg     config.SystemConfig
	store   *coretest.ContainerStoreFake
	docker  *coremocks.MemoryPressureDockerAPI
	metrics *coremocks.MemoryPressureHostMetrics
	quotas  *coretest.UserQuotaStoreFake
	worker  *service.MemoryPressureWorker
	quota   map[string]int64
	owners  map[string]uuid.UUID
	names   map[uuid.UUID]string
	// usedMiB and cpuNanos are each workload's working set and cumulative
	// CPU time, by name.
	usedMiB  map[string]int64
	cpuNanos map[string]int64
	// limits and cpuQuota are each workload's current memory limit and CPU
	// quota; sent and cpuSent name those updated, and updates counts the
	// updates each got.
	limits        map[string]int64
	cpuQuota      map[string]int64
	cpuPeriod     map[string]int64
	periodSent    map[string]int64
	sent          map[string]bool
	cpuSent       map[string]bool
	updates       map[string]int
	inspected     []string
	builds        []model.BuildContainer
	stopper       *coremocks.MemoryPressureBuildStopper
	buildsStopped []string
}

func newBoostPass(t *testing.T, cfg config.SystemConfig, quotaMiB map[string]int64) *boostPass {
	t.Helper()
	p := &boostPass{
		t: t, cfg: cfg, store: coretest.NewContainerStoreFake(),
		docker:  coremocks.NewMemoryPressureDockerAPI(t),
		metrics: coremocks.NewMemoryPressureHostMetrics(t),
		quotas:  coretest.NewUserQuotaStoreFake(),
		quota:   quotaMiB, owners: map[string]uuid.UUID{}, names: map[uuid.UUID]string{},
		usedMiB: map[string]int64{}, cpuNanos: map[string]int64{},
		limits: map[string]int64{}, cpuQuota: map[string]int64{}, cpuPeriod: map[string]int64{}, periodSent: map[string]int64{},
		sent: map[string]bool{}, cpuSent: map[string]bool{}, updates: map[string]int{},
		stopper: coremocks.NewMemoryPressureBuildStopper(t),
	}
	p.metrics.EXPECT().GetTotalMemory().Return(8192*pressureMiB, nil).Maybe()
	builds := coremocks.NewActiveSystemBuildCounter(t)
	builds.EXPECT().CountActive(mock.Anything).RunAndReturn(
		func(context.Context) (int, error) { return len(p.builds), nil }).Maybe()
	p.docker.EXPECT().ListRunningBuildContainers(mock.Anything).RunAndReturn(
		func(context.Context) ([]model.BuildContainer, error) { return p.builds, nil }).Maybe()
	static := coretest.StaticConfig{Config: cfg}
	quotas, err := userquota.New(p.quotas, static)
	require.NoError(t, err)
	worker, err := service.NewMemoryPressureWorker(service.MemoryPressureWorkerDeps{
		Repository:   p.store,
		Docker:       p.docker,
		Metrics:      p.metrics,
		Builds:       builds,
		BuildStopper: p.stopper,
		Quotas:       quotas,
		Config:       static,
		Logger:       slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)
	p.worker = worker
	return p
}

// add saves a running container and scripts Docker for it.
func (p *boostPass) add(bc boostContainer) {
	t := p.t
	ctx := context.Background()
	owner := p.owner(cmp.Or(bc.owner, bc.name))
	c := model.Container{
		ID:                    uuid.New(),
		OwnerID:               owner,
		Name:                  bc.name,
		DockerID:              "docker-" + bc.name,
		DockerGeneration:      1,
		Status:                model.ContainerStatusRunning,
		DesiredStatus:         model.ContainerStatusRunning,
		BaseMemoryReservation: bc.reservation(),
	}
	require.NoError(t, p.store.Save(ctx, c))
	p.names[c.ID] = bc.name
	if bc.busy {
		op := model.ResourceOperation{ID: uuid.New(), ResourceType: model.ResourceTypeContainer, ResourceID: c.ID, OwnerID: c.OwnerID, Operation: model.OperationExpose, Status: model.OperationStatusPending}
		require.NoError(t, p.store.QueueContainerOperation(ctx, c.ID, model.ContainerStatusRunning, model.ContainerStatusRunning, op, model.ContainerLifecycleOutbox{ID: uuid.New(), OperationID: op.ID, ContainerID: c.ID, Status: model.ContainerOutboxStatusPublished}))
	}
	p.script(bc.name, c.DockerID, bc.usedMiB, bc.limit(), p.cfg.ContainerCPUPeriod, func(update model.ContainerResourceUpdate) error {
		want := model.ContainerResourceUpdate{CPUQuota: update.CPUQuota, CPUPeriod: update.CPUPeriod}
		if update.MemoryLimitBytes != 0 {
			want.MemoryLimitBytes = update.MemoryLimitBytes
			want.MemoryReservation = bc.reservation()
			want.MemorySwapMultiplier = p.cfg.ContainerMemorySwapMultiplier
		}
		require.Equal(t, want, update, "swap follows a memory limit, the reservation is resent with it, and CPU shares and period are left as they are")
		return bc.updateErr
	})
}

// owner returns the user named key, creating it with its custom quota if it
// has one.
func (p *boostPass) owner(key string) uuid.UUID {
	owner, ok := p.owners[key]
	if !ok {
		owner = uuid.New()
		p.owners[key] = owner
		if quota, custom := p.quota[key]; custom {
			require.NoError(p.t, p.quotas.SetCustomUserQuota(context.Background(), owner, model.UserQuota{CPUMillicores: 1000, RAMMB: quota, DiskMB: 1024}))
		}
	}
	return owner
}

// addBuild runs a build container and scripts Docker for it. Its build slot
// counts as active.
func (p *boostPass) addBuild(bb boostBuild) {
	t := p.t
	dockerID := "kaniko-" + bb.name
	buildID := uuid.New()
	p.builds = append(p.builds, model.BuildContainer{DockerID: dockerID, BuildID: buildID, OwnerID: p.owner(cmp.Or(bb.owner, bb.name))})
	p.script(bb.name, dockerID, bb.usedMiB, bb.limit(), p.cfg.BuildCPUPeriod, func(update model.ContainerResourceUpdate) error {
		want := model.ContainerResourceUpdate{CPUQuota: update.CPUQuota, CPUPeriod: update.CPUPeriod}
		if update.MemoryLimitBytes != 0 {
			want.MemoryLimitBytes = update.MemoryLimitBytes
			want.MemorySwapMultiplier = p.cfg.BuildMemorySwapMultiplier
		}
		require.Equal(t, want, update, "swap follows the build multiplier; reservation, CPU shares and period are left as they are")
		return nil
	})
	p.stopper.EXPECT().StopBuildForResourceExhaustion(mock.Anything, buildID).RunAndReturn(
		func(context.Context, uuid.UUID) (bool, error) {
			if bb.stopErr != nil {
				return false, bb.stopErr
			}
			p.buildsStopped = append(p.buildsStopped, bb.name)
			return true, nil
		}).Maybe()
}

// script has Docker report a workload's usage, current limit and CPU quota,
// and record the updates check accepts.
func (p *boostPass) script(name, dockerID string, usedMiB, limit, cpuPeriod int64, check func(model.ContainerResourceUpdate) error) {
	p.usedMiB[name] = usedMiB
	p.limits[name] = limit
	p.cpuPeriod[name] = cpuPeriod
	p.docker.EXPECT().GetContainerUsage(mock.Anything, dockerID).RunAndReturn(
		func(context.Context, string) (model.ContainerUsage, error) {
			return model.ContainerUsage{
				WorkingSetBytes:     p.usedMiB[name] * pressureMiB,
				MemoryLimitBytes:    p.limits[name],
				CPUUsageNanoseconds: p.cpuNanos[name],
			}, nil
		}).Maybe()
	p.docker.EXPECT().InspectContainer(mock.Anything, dockerID).RunAndReturn(
		func(context.Context, string) (model.ContainerInspection, error) {
			p.inspected = append(p.inspected, name)
			return model.ContainerInspection{MemoryLimitBytes: p.limits[name], CPUQuota: p.cpuQuota[name], CPUPeriod: p.cpuPeriod[name]}, nil
		}).Maybe()
	p.docker.EXPECT().UpdateContainerResources(mock.Anything, dockerID, mock.Anything).RunAndReturn(
		func(_ context.Context, _ string, update model.ContainerResourceUpdate) error {
			p.updates[name]++
			require.False(p.t, update.CPUPeriod != 0 && update.CPUQuota == 0, "a CPU period goes only with a quota")
			err := check(update)
			// A failed update is recorded too: the test sees what was sent.
			if update.MemoryLimitBytes != 0 {
				p.limits[name] = update.MemoryLimitBytes
				p.sent[name] = true
			}
			if update.CPUQuota != 0 {
				p.cpuQuota[name] = update.CPUQuota
				p.cpuSent[name] = true
			}
			if update.CPUPeriod != 0 {
				p.cpuPeriod[name] = update.CPUPeriod
				p.periodSent[name] = update.CPUPeriod
			}
			return err
		}).Maybe()
}

func (p *boostPass) updatedMiB() map[string]int64 {
	got := map[string]int64{}
	for name := range p.sent {
		got[name] = p.limits[name] / pressureMiB
	}
	return got
}

func (p *boostPass) updatedQuotas() map[string]int64 {
	got := map[string]int64{}
	for name := range p.cpuSent {
		got[name] = p.cpuQuota[name]
	}
	return got
}

func (p *boostPass) stopped() []stopOf {
	outbox, err := p.store.LeasePendingContainerOutbox(context.Background(), 10)
	require.NoError(p.t, err)
	var stops []stopOf
	for _, record := range outbox {
		var msg queue.ContainerLifecycleMessage
		require.NoError(p.t, json.Unmarshal(record.Payload, &msg))
		require.Equal(p.t, model.OperationStop, msg.Operation)
		stops = append(stops, stopOf{p.names[record.ContainerID], msg.Reason})
	}
	return stops
}
