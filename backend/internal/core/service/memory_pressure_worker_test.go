package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

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
				docker.EXPECT().GetContainerMemoryWorkingSet(mock.Anything, c.DockerID).Return(pc.usedMiB*pressureMiB, pc.readErr).Once()
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
			Config:     testConfig(),
		}
	}
	depstest.RequireRejectsMissing(t, valid, service.NewMemoryPressureWorker, map[string]string{
		"Repository": "repository",
		"Docker":     "docker",
		"Metrics":    "metrics",
		"Builds":     "builds",
		"Config":     "config",
	})
}
