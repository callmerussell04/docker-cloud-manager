package service

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/google/uuid"
)

// MemoryPressureStopReason is the last error of a container the memory
// pressure worker stopped. It names no other user's usage.
const MemoryPressureStopReason = "stopped by the platform: the host ran short of memory while this container was using more than its memory reservation"

type MemoryPressureContainerRepo interface {
	GetRunning(ctx context.Context) ([]model.Container, error)
	containerStopQueue
}

// containerStopQueue is what queueing a platform stop needs.
type containerStopQueue interface {
	HasActiveOperation(ctx context.Context, resourceType string, resourceID uuid.UUID) (bool, error)
	QueueContainerOperation(ctx context.Context, id uuid.UUID, status string, desiredStatus string, op model.ResourceOperation, outbox model.ContainerLifecycleOutbox) error
}

type MemoryPressureDockerAPI interface {
	GetContainerMemoryWorkingSet(ctx context.Context, dockerID string) (int64, error)
}

type MemoryPressureHostMetrics interface {
	GetTotalMemory() (int64, error)
}

type MemoryPressureConfigProvider interface {
	Get() config.SystemConfig
}

// MemoryPressureWorker keeps the user memory pool from running out. The pool
// is host memory less the memory held back for the system and for build
// slots. Admission keeps reservations within it, but containers may use up to
// their limit, so their use together can exceed it. When less than the
// configured floor is free, the worker stops containers that use more than
// they reserved, largest excess first. It never stops a container that is
// within its reservation.
type MemoryPressureWorker struct {
	repo    MemoryPressureContainerRepo
	docker  MemoryPressureDockerAPI
	metrics MemoryPressureHostMetrics
	builds  activeSystemBuildCounter
	cfg     MemoryPressureConfigProvider
	logger  *slog.Logger
}

type MemoryPressureWorkerDeps struct {
	Repository MemoryPressureContainerRepo
	Docker     MemoryPressureDockerAPI
	Metrics    MemoryPressureHostMetrics
	Builds     activeSystemBuildCounter
	Config     MemoryPressureConfigProvider
	Logger     *slog.Logger
}

func NewMemoryPressureWorker(deps MemoryPressureWorkerDeps) (*MemoryPressureWorker, error) {
	required := []struct {
		name  string
		value any
	}{
		{"repository", deps.Repository}, {"docker", deps.Docker}, {"metrics", deps.Metrics},
		{"builds", deps.Builds}, {"config", deps.Config},
	}
	for _, dependency := range required {
		if dependency.value == nil || isNilServiceDependency(dependency.value) {
			return nil, fmt.Errorf("memory pressure worker: %s is required", dependency.name)
		}
	}
	return &MemoryPressureWorker{
		repo:    deps.Repository,
		docker:  deps.Docker,
		metrics: deps.Metrics,
		builds:  deps.Builds,
		cfg:     deps.Config,
		logger:  logging.WithComponent(deps.Logger, "memory_pressure_worker"),
	}, nil
}

// Run checks for memory pressure once per MemoryPressureIntervalSeconds,
// read fresh before each wait.
func (w *MemoryPressureWorker) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "memory pressure worker started")
	runPeriodically(ctx, func() int64 { return w.cfg.Get().MemoryPressureIntervalSeconds }, w.RelievePressure)
	w.logger.InfoContext(ctx, "memory pressure worker stopped")
}

// containerOverReservation is a running container using more memory than it
// reserved.
type containerOverReservation struct {
	container   model.Container
	workingSet  int64
	reservation int64
}

func (o containerOverReservation) excess() int64 { return o.workingSet - o.reservation }

// RelievePressure measures the running containers once and, when the user
// memory pool has less than MemoryPressureMinFreeBytes free, queues stops for
// containers over their reservation until the stops cover the shortfall. A
// zero floor turns the check off. A container whose usage cannot be read
// counts as using nothing and is not stopped.
func (w *MemoryPressureWorker) RelievePressure(ctx context.Context) {
	cfg := w.cfg.Get()
	if cfg.MemoryPressureMinFreeBytes <= 0 {
		return
	}
	pool, err := w.userMemoryPool(ctx, cfg)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to size the user memory pool", "error", err)
		return
	}
	running, err := w.repo.GetRunning(ctx)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to fetch running containers", "error", err)
		return
	}

	var used int64
	var over []containerOverReservation
	for _, c := range running {
		if c.DockerID == "" {
			continue
		}
		workingSet, err := w.docker.GetContainerMemoryWorkingSet(ctx, c.DockerID)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			w.logger.WarnContext(ctx, "failed to read container memory usage", "container_id", c.ID, "error", err)
			continue
		}
		used += workingSet
		reservation := normalizeMemoryReservation(c.BaseMemoryReservation, cfg)
		if workingSet > reservation {
			over = append(over, containerOverReservation{container: c, workingSet: workingSet, reservation: reservation})
		}
	}

	shortfall := cfg.MemoryPressureMinFreeBytes - (pool - used)
	if shortfall <= 0 {
		return
	}
	if len(over) == 0 {
		w.logger.WarnContext(ctx, "user memory pool is short but no container exceeds its reservation",
			"pool_bytes", pool, "used_bytes", used, "min_free_bytes", cfg.MemoryPressureMinFreeBytes)
		return
	}
	slices.SortStableFunc(over, func(a, b containerOverReservation) int { return cmp.Compare(b.excess(), a.excess()) })
	for _, o := range over {
		if shortfall <= 0 {
			break
		}
		o.container.Status = model.ContainerStatusRunning
		if err := queueContainerStop(ctx, w.repo, o.container, MemoryPressureStopReason); err != nil {
			if errors.Is(err, apperrors.ErrConflict) {
				w.logger.DebugContext(ctx, "container over its reservation already has an active operation", "container_id", o.container.ID)
				continue
			}
			w.logger.WarnContext(ctx, "failed to queue memory pressure stop", "container_id", o.container.ID, "error", err)
			continue
		}
		w.logger.WarnContext(ctx, "container stop queued due to memory pressure",
			"container_id", o.container.ID, "user_id", o.container.OwnerID,
			"working_set_bytes", o.workingSet, "reservation_bytes", o.reservation)
		// Stopping a container frees everything it holds, not only its excess.
		shortfall -= o.workingSet
	}
}

// userMemoryPool is the host memory user containers may use together.
func (w *MemoryPressureWorker) userMemoryPool(ctx context.Context, cfg config.SystemConfig) (int64, error) {
	total, err := w.metrics.GetTotalMemory()
	if err != nil {
		return 0, fmt.Errorf("read host memory: %w", err)
	}
	activeBuilds, err := w.builds.CountActive(ctx)
	if err != nil {
		return 0, fmt.Errorf("count active builds: %w", err)
	}
	return total - cfg.ReservedSystemMemory - reservedBuildSlots(int64(activeBuilds), cfg)*cfg.BuildMemoryBytes, nil
}

// queueContainerStop queues a stop the platform decided on, unless the
// container already has an active operation, which is an ErrConflict. A
// non-empty reason becomes the stopped container's last error.
func queueContainerStop(ctx context.Context, repo containerStopQueue, c model.Container, reason string) error {
	active, err := repo.HasActiveOperation(ctx, model.ResourceTypeContainer, c.ID)
	if err != nil {
		return err
	}
	if active {
		return apperrors.New(apperrors.ErrConflict, "resource operation is already in progress")
	}
	op := model.ResourceOperation{
		ID:           uuid.New(),
		ResourceType: model.ResourceTypeContainer,
		ResourceID:   c.ID,
		OwnerID:      c.OwnerID,
		Operation:    model.OperationStop,
		Status:       model.OperationStatusPending,
	}
	msg := queue.ContainerLifecycleMessage{
		OperationID:    op.ID.String(),
		ContainerID:    c.ID.String(),
		OwnerID:        c.OwnerID.String(),
		Operation:      model.OperationStop,
		RequestID:      logging.RequestIDFromContext(ctx),
		CreatedAt:      time.Now().Unix(),
		PreviousStatus: c.Status,
		Reason:         reason,
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal platform stop message: %w", err)
	}
	outbox := model.ContainerLifecycleOutbox{
		ID:          uuid.New(),
		OperationID: op.ID,
		ContainerID: c.ID,
		Exchange:    queue.ContainerExchangeName,
		RoutingKey:  queue.ContainerRoutingKey,
		Payload:     payload,
		Status:      model.ContainerOutboxStatusPending,
	}
	return repo.QueueContainerOperation(ctx, c.ID, model.ContainerStatusStopping, model.ContainerStatusExited, op, outbox)
}
