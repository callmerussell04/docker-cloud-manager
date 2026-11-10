package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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
	GetContainerUsage(ctx context.Context, dockerID string) (model.ContainerUsage, error)
	UpdateContainerResources(ctx context.Context, dockerID string, resources model.ContainerResourceUpdate) error
}

type MemoryPressureHostMetrics interface {
	GetTotalMemory() (int64, error)
}

type MemoryPressureConfigProvider interface {
	Get() config.SystemConfig
}

// MemoryPressureWorker keeps the user memory pool from running out and lends
// what is idle. The pool is host memory less the memory held back for the
// system and for build slots. Admission keeps reservations within it, but
// containers may use up to their limit, so their use together can exceed it.
//
// Each pass measures every running container and acts on the free part of
// the pool. With memory boost on, a container close to its limit while the
// pool has plenty free gets its limit raised, up to its boost ceiling; when
// the pool runs low, raised limits are lowered to what their containers use
// plus a margin. When less than the stop floor is free, the worker stops
// containers using borrowed memory, then containers using more than they
// reserved, largest first. It never stops a container that is within its
// reservation. This worker is the only thing that lowers a running
// container's memory limit.
type MemoryPressureWorker struct {
	repo    MemoryPressureContainerRepo
	docker  MemoryPressureDockerAPI
	metrics MemoryPressureHostMetrics
	builds  activeSystemBuildCounter
	users   UserInfoProvider
	cfg     MemoryPressureConfigProvider
	logger  *slog.Logger

	// mu serializes passes; cooldown is the passes left, by Docker ID,
	// before a lowered limit may be raised again.
	mu       sync.Mutex
	cooldown map[string]int
}

type MemoryPressureWorkerDeps struct {
	Repository MemoryPressureContainerRepo
	Docker     MemoryPressureDockerAPI
	Metrics    MemoryPressureHostMetrics
	Builds     activeSystemBuildCounter
	Users      UserInfoProvider
	Config     MemoryPressureConfigProvider
	Logger     *slog.Logger
}

func NewMemoryPressureWorker(deps MemoryPressureWorkerDeps) (*MemoryPressureWorker, error) {
	required := []struct {
		name  string
		value any
	}{
		{"repository", deps.Repository}, {"docker", deps.Docker}, {"metrics", deps.Metrics},
		{"builds", deps.Builds}, {"users", deps.Users}, {"config", deps.Config},
	}
	for _, dependency := range required {
		if dependency.value == nil || isNilServiceDependency(dependency.value) {
			return nil, fmt.Errorf("memory pressure worker: %s is required", dependency.name)
		}
	}
	return &MemoryPressureWorker{
		repo:     deps.Repository,
		docker:   deps.Docker,
		metrics:  deps.Metrics,
		builds:   deps.Builds,
		users:    deps.Users,
		cfg:      deps.Config,
		logger:   logging.WithComponent(deps.Logger, "memory_pressure_worker"),
		cooldown: map[string]int{},
	}, nil
}

// Run checks for memory pressure once per MemoryPressureIntervalSeconds,
// read fresh before each wait.
func (w *MemoryPressureWorker) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "memory pressure worker started")
	runPeriodically(ctx, func() int64 { return w.cfg.Get().MemoryPressureIntervalSeconds }, w.RelievePressure)
	w.logger.InfoContext(ctx, "memory pressure worker stopped")
}

// RelievePressure runs one pass: it measures the running containers once,
// decides with planMemoryBoost, and applies the stops and limit changes. A
// zero MemoryPressureMinFreeBytes turns the worker off. A container whose
// usage cannot be read counts as using nothing and is left alone.
func (w *MemoryPressureWorker) RelievePressure(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
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

	in := memoryBoostInput{
		pool:         pool,
		minFree:      cfg.MemoryPressureMinFreeBytes,
		boostMinFree: cfg.BoostMinFreeMemoryBytes,
		boostEnabled: cfg.MemoryBoostMultiplier > cfg.MaxBurstMultiplier,
	}
	nextCooldown := map[string]int{}
	for _, c := range running {
		if c.DockerID == "" {
			continue
		}
		passesLeft := w.cooldown[c.DockerID]
		if passesLeft > 1 {
			nextCooldown[c.DockerID] = passesLeft - 1
		}
		usage, err := w.docker.GetContainerUsage(ctx, c.DockerID)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			w.logger.WarnContext(ctx, "failed to read container memory usage", "container_id", c.ID, "error", err)
			continue
		}
		standard := containerResources(c, cfg)
		in.workloads = append(in.workloads, memoryWorkload{
			container:     c,
			reservation:   standard.MemoryReservation,
			standardLimit: standard.MemoryLimitBytes,
			ceiling:       memoryBoostCeiling(c, cfg),
			workingSet:    usage.WorkingSetBytes,
			limit:         usage.MemoryLimitBytes,
			coolingDown:   passesLeft > 0,
		})
	}
	w.cooldown = nextCooldown
	if in.quotas, err = w.memoryQuotas(ctx, in.growCandidateOwners()); err != nil {
		return
	}

	plan := planMemoryBoost(in)
	stopped := w.queueStops(ctx, plan)
	for _, change := range plan.changes {
		if !stopped[change.workload.container.ID] {
			w.changeLimit(ctx, change, cfg)
		}
	}
}

// memoryQuotas reads the memory quota of each owner. An owner whose quota
// cannot be read gets none, so nothing is lent to them this pass. It fails
// only when ctx is done.
func (w *MemoryPressureWorker) memoryQuotas(ctx context.Context, owners []uuid.UUID) (map[uuid.UUID]int64, error) {
	quotas := make(map[uuid.UUID]int64, len(owners))
	for _, owner := range owners {
		user, err := w.users.GetUser(ctx, owner)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			w.logger.WarnContext(ctx, "failed to read user memory quota", "user_id", owner, "error", err)
			continue
		}
		quotas[owner] = user.QuotaRAMMB * bytesPerMB
	}
	return quotas, nil
}

// queueStops queues the plan's stops in order until they cover its
// shortfall, passing over containers with an active operation, and returns
// the containers it stopped.
func (w *MemoryPressureWorker) queueStops(ctx context.Context, plan memoryBoostPlan) map[uuid.UUID]bool {
	stopped := map[uuid.UUID]bool{}
	if plan.shortfall <= 0 {
		return stopped
	}
	if len(plan.stops) == 0 {
		w.logger.WarnContext(ctx, "user memory pool is short but no container exceeds its reservation",
			"free_bytes", plan.free, "min_free_bytes", plan.free+plan.shortfall)
		return stopped
	}
	shortfall := plan.shortfall
	for _, stop := range plan.stops {
		if shortfall <= 0 {
			break
		}
		c := stop.workload.container
		c.Status = model.ContainerStatusRunning
		if err := queueContainerStop(ctx, w.repo, c, stop.reason); err != nil {
			if errors.Is(err, apperrors.ErrConflict) {
				w.logger.DebugContext(ctx, "container to stop already has an active operation", "container_id", c.ID)
				continue
			}
			w.logger.WarnContext(ctx, "failed to queue memory pressure stop", "container_id", c.ID, "error", err)
			continue
		}
		w.logger.WarnContext(ctx, "container stop queued due to memory pressure",
			"container_id", c.ID, "user_id", c.OwnerID,
			"working_set_bytes", stop.workload.workingSet, "reservation_bytes", stop.workload.reservation,
			"standard_limit_bytes", stop.workload.standardLimit)
		stopped[c.ID] = true
		// Stopping a container frees everything it holds, not only its excess.
		shortfall -= stop.workload.workingSet
	}
	return stopped
}

// changeLimit sets a container's memory limit, unless it has an active
// operation of its own. Swap follows the limit; CPU is left as it is. A
// lowered limit starts the cooldown.
func (w *MemoryPressureWorker) changeLimit(ctx context.Context, change memoryLimitChange, cfg config.SystemConfig) {
	c := change.workload.container
	active, err := w.repo.HasActiveOperation(ctx, model.ResourceTypeContainer, c.ID)
	if err != nil {
		w.logger.WarnContext(ctx, "failed to check container operations before a limit change", "container_id", c.ID, "error", err)
		return
	}
	if active {
		w.logger.DebugContext(ctx, "container with an active operation keeps its memory limit", "container_id", c.ID)
		return
	}
	update := model.ContainerResourceUpdate{
		MemoryLimitBytes:     change.limit,
		MemoryReservation:    change.workload.reservation,
		MemorySwapMultiplier: cfg.ContainerMemorySwapMultiplier,
	}
	if err := w.docker.UpdateContainerResources(ctx, c.DockerID, update); err != nil {
		w.logger.WarnContext(ctx, "failed to change container memory limit", "container_id", c.ID, "error", err)
		return
	}
	message := "container memory limit raised"
	if change.limit < change.workload.limit {
		message = "container memory limit lowered"
		w.cooldown[c.DockerID] = boostCooldownPasses
	}
	w.logger.InfoContext(ctx, message,
		"container_id", c.ID, "user_id", c.OwnerID,
		"working_set_bytes", change.workload.workingSet,
		"old_limit_bytes", change.workload.limit, "new_limit_bytes", change.limit,
		"standard_limit_bytes", change.workload.standardLimit)
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
