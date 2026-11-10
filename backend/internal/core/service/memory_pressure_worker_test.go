package service_test

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
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
			metrics := coremocks.NewHostMetricsProvider(t)
			metrics.EXPECT().GetTotalMemory().Return(4096*pressureMiB, nil).Maybe()
			builds := coremocks.NewActiveSystemBuildCounter(t)
			builds.EXPECT().CountActive(mock.Anything).Return(tc.activeBuilds, nil).Maybe()
			worker, err := service.NewMemoryPressureWorker(service.MemoryPressureWorkerDeps{
				Repository: store,
				Docker:     docker,
				Metrics:    metrics,
				Builds:     builds,
				Users:      coremocks.NewUserInfoProvider(t),
				Config:     coretest.StaticConfig{Config: cfg},
				Logger:     slog.New(slog.DiscardHandler),
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
			Repository: coretest.NewContainerStoreFake(),
			Docker:     coremocks.NewMemoryPressureDockerAPI(t),
			Metrics:    coremocks.NewHostMetricsProvider(t),
			Builds:     coremocks.NewActiveSystemBuildCounter(t),
			Users:      coremocks.NewUserInfoProvider(t),
			Config:     testConfig(),
		}
	}
	depstest.RequireRejectsMissing(t, valid, service.NewMemoryPressureWorker, map[string]string{
		"Repository": "repository",
		"Docker":     "docker",
		"Metrics":    "metrics",
		"Builds":     "builds",
		"Users":      "users",
		"Config":     "config",
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

// The host has 8 GiB with 1 GiB held back for the system, so user containers
// share a 7 GiB pool. Boosts are granted while at least 2 GiB of it stays
// free, and containers are stopped when less than 512 MiB is free. A limit
// grows by half its standard limit per pass.
func TestMemoryPressureWorkerBoostsAndReclaimsMemory(t *testing.T) {
	for _, tc := range []struct {
		name       string
		containers []boostContainer
		// boostMultiplier defaults to 8.
		boostMultiplier int64
		// quotaMiB is the memory quota of a named owner; others have 64 GiB.
		quotaMiB map[string]int64
		// wantLimitsMiB is every limit update sent to Docker.
		wantLimitsMiB map[string]int64
		wantStopped   []stopOf
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
			name:            "a boost multiplier at the burst multiplier makes no limit updates",
			boostMultiplier: boostBurstMultiplier,
			containers: []boostContainer{
				{name: "near", usedMiB: 900},
				{name: "boosted", usedMiB: 300, limitMiB: 2048},
				{name: "filler", reservationMiB: 2048, usedMiB: 5000},
			},
			wantLimitsMiB: map[string]int64{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := boostTestConfig()
			if tc.boostMultiplier != 0 {
				cfg.MemoryBoostMultiplier = tc.boostMultiplier
			}
			pass := newBoostPass(t, cfg, tc.quotaMiB)
			for _, bc := range tc.containers {
				pass.add(bc, func(*mock.Call) {})
			}

			pass.worker.RelievePressure(ctx)

			require.Equal(t, tc.wantLimitsMiB, pass.updatedMiB(), "limit updates sent to Docker")
			require.Equal(t, tc.wantStopped, pass.stopped(), "stops queued, in order")
		})
	}
}

// A limit lowered under pressure is not raised again for six passes, so it
// does not swing while the host hovers around the boost floor.
func TestMemoryPressureWorkerDoesNotRaiseALoweredLimitWithinTheCooldown(t *testing.T) {
	ctx := context.Background()
	pass := newBoostPass(t, boostTestConfig(), nil)
	// The first pass sees a busy host; the rest see the filler gone.
	passes := 0
	usage := func(name string) model.ContainerUsage {
		switch {
		case name == "filler" && passes == 1:
			return model.ContainerUsage{WorkingSetBytes: 5000 * pressureMiB, MemoryLimitBytes: 8192 * pressureMiB}
		case name == "filler":
			return model.ContainerUsage{WorkingSetBytes: 10 * pressureMiB, MemoryLimitBytes: 8192 * pressureMiB}
		case passes == 1:
			return model.ContainerUsage{WorkingSetBytes: 1000 * pressureMiB, MemoryLimitBytes: 2048 * pressureMiB}
		default:
			return model.ContainerUsage{WorkingSetBytes: 1000 * pressureMiB, MemoryLimitBytes: pass.limits["borrower"]}
		}
	}
	for _, bc := range []boostContainer{{name: "borrower"}, {name: "filler", reservationMiB: 2048}} {
		pass.add(bc, func(call *mock.Call) {
			call.Unset()
			pass.docker.EXPECT().GetContainerUsage(mock.Anything, "docker-"+bc.name).RunAndReturn(
				func(context.Context, string) (model.ContainerUsage, error) { return usage(bc.name), nil })
		})
	}

	var history []int64
	for passes = 1; passes <= 8; passes++ {
		pass.worker.RelievePressure(ctx)
		history = append(history, pass.limits["borrower"]/pressureMiB)
	}

	require.Equal(t, []int64{1100, 1100, 1100, 1100, 1100, 1100, 1100, 1612}, history,
		"lowered to usage plus 10%, held for six passes, then raised one step")
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
	return cfg
}

// boostPass is a memory pressure worker over the container store fake and a
// mocked Docker that records the limits it is sent.
type boostPass struct {
	t      *testing.T
	cfg    config.SystemConfig
	store  *coretest.ContainerStoreFake
	docker *coremocks.MemoryPressureDockerAPI
	users  *coremocks.UserInfoProvider
	worker *service.MemoryPressureWorker
	quota  map[string]int64
	owners map[string]uuid.UUID
	names  map[uuid.UUID]string
	// limits is each container's current limit; sent names those updated.
	limits map[string]int64
	sent   map[string]bool
}

func newBoostPass(t *testing.T, cfg config.SystemConfig, quotaMiB map[string]int64) *boostPass {
	t.Helper()
	p := &boostPass{
		t: t, cfg: cfg, store: coretest.NewContainerStoreFake(),
		docker: coremocks.NewMemoryPressureDockerAPI(t),
		users:  coremocks.NewUserInfoProvider(t),
		quota:  quotaMiB, owners: map[string]uuid.UUID{}, names: map[uuid.UUID]string{},
		limits: map[string]int64{}, sent: map[string]bool{},
	}
	metrics := coremocks.NewHostMetricsProvider(t)
	metrics.EXPECT().GetTotalMemory().Return(8192*pressureMiB, nil).Maybe()
	builds := coremocks.NewActiveSystemBuildCounter(t)
	builds.EXPECT().CountActive(mock.Anything).Return(0, nil).Maybe()
	worker, err := service.NewMemoryPressureWorker(service.MemoryPressureWorkerDeps{
		Repository: p.store,
		Docker:     p.docker,
		Metrics:    metrics,
		Builds:     builds,
		Users:      p.users,
		Config:     coretest.StaticConfig{Config: cfg},
		Logger:     slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)
	p.worker = worker
	return p
}

// add saves a running container and scripts Docker for it. tweakUsage may
// replace the usage expectation it is given.
func (p *boostPass) add(bc boostContainer, tweakUsage func(*mock.Call)) {
	t := p.t
	ctx := context.Background()
	ownerKey := cmp.Or(bc.owner, bc.name)
	owner, ok := p.owners[ownerKey]
	if !ok {
		owner = uuid.New()
		p.owners[ownerKey] = owner
		quota := cmp.Or(p.quota[ownerKey], 64*1024)
		p.users.EXPECT().GetUser(mock.Anything, owner).Return(model.UserInfo{ID: owner, QuotaRAMMB: quota}, nil).Maybe()
	}
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
	p.limits[bc.name] = bc.limit()
	if bc.busy {
		op := model.ResourceOperation{ID: uuid.New(), ResourceType: model.ResourceTypeContainer, ResourceID: c.ID, OwnerID: c.OwnerID, Operation: model.OperationExpose, Status: model.OperationStatusPending}
		require.NoError(t, p.store.QueueContainerOperation(ctx, c.ID, model.ContainerStatusRunning, model.ContainerStatusRunning, op, model.ContainerLifecycleOutbox{ID: uuid.New(), OperationID: op.ID, ContainerID: c.ID, Status: model.ContainerOutboxStatusPublished}))
	}
	tweakUsage(p.docker.EXPECT().GetContainerUsage(mock.Anything, c.DockerID).
		Return(model.ContainerUsage{WorkingSetBytes: bc.usedMiB * pressureMiB, MemoryLimitBytes: bc.limit()}, nil).Once())
	p.docker.EXPECT().UpdateContainerResources(mock.Anything, c.DockerID, mock.Anything).RunAndReturn(
		func(_ context.Context, _ string, update model.ContainerResourceUpdate) error {
			require.Equal(t, model.ContainerResourceUpdate{
				MemoryLimitBytes:     update.MemoryLimitBytes,
				MemoryReservation:    bc.reservation(),
				MemorySwapMultiplier: p.cfg.ContainerMemorySwapMultiplier,
			}, update, "only the memory limit changes; swap follows it and CPU is left as it is")
			p.limits[bc.name] = update.MemoryLimitBytes
			p.sent[bc.name] = true
			return bc.updateErr
		}).Maybe()
}

func (p *boostPass) updatedMiB() map[string]int64 {
	got := map[string]int64{}
	for name := range p.sent {
		got[name] = p.limits[name] / pressureMiB
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
