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
	ListRunningBuildContainers(ctx context.Context) ([]model.BuildContainer, error)
	KillContainer(ctx context.Context, dockerID string) error
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
// Each pass measures every running container and build container and acts on the free part of the pool. With
// memory boost on, a workload close to its limit while the pool has plenty
// free gets its limit raised, up to its boost ceiling; when the pool runs
// low, raised limits are lowered to what their workloads use plus a margin.
// When less than the stop floor is free, the worker stops containers and
// kills builds using borrowed memory, then those using more than they
// reserved, largest first. It never stops a container within its
// reservation or a build within its build memory. This worker is the only
// thing that lowers a running workload's memory limit.
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

// RelievePressure runs one pass: it measures the running workloads once,
// decides with planMemoryBoost, and applies the stops and limit changes. A
// zero MemoryPressureMinFreeBytes turns the worker off. A workload whose
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
		standard := containerResources(c, cfg)
		workload := memoryWorkload{
			container:     c,
			reservation:   standard.MemoryReservation,
			standardLimit: standard.MemoryLimitBytes,
			ceiling:       memoryBoostCeiling(c, cfg),
		}
		if !w.measure(ctx, &workload, nextCooldown) {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		in.workloads = append(in.workloads, workload)
	}
	builds, err := w.docker.ListRunningBuildContainers(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		// Containers can still be relieved; builds sit this pass out.
		w.logger.WarnContext(ctx, "failed to list running build containers", "error", err)
	}
	for _, b := range builds {
		workload := memoryWorkload{
			build:         &b,
			reservation:   cfg.BuildMemoryBytes,
			standardLimit: cfg.BuildMemoryBytes,
			ceiling:       cfg.BuildMemoryBytes * cfg.MemoryBoostMultiplier,
		}
		if !w.measure(ctx, &workload, nextCooldown) {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		in.workloads = append(in.workloads, workload)
	}
	w.cooldown = nextCooldown
	if in.quotas, err = w.memoryQuotas(ctx, in.growCandidateOwners()); err != nil {
		return
	}

	plan := planMemoryBoost(in)
	stopped := w.applyStops(ctx, plan)
	for _, change := range plan.changes {
		if !stopped[change.workload.dockerID()] {
			w.changeLimit(ctx, change, cfg)
		}
	}
}

// measure reads a workload's working set and current limit and carries its
// cooldown into nextCooldown. It reports false when the usage cannot be read.
func (w *MemoryPressureWorker) measure(ctx context.Context, workload *memoryWorkload, nextCooldown map[string]int) bool {
	dockerID := workload.dockerID()
	passesLeft := w.cooldown[dockerID]
	if passesLeft > 1 {
		nextCooldown[dockerID] = passesLeft - 1
	}
	usage, err := w.docker.GetContainerUsage(ctx, dockerID)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.WarnContext(ctx, "failed to read workload memory usage", workloadLogAttrs(*workload, "error", err)...)
		}
		return false
	}
	workload.workingSet = usage.WorkingSetBytes
	workload.limit = usage.MemoryLimitBytes
	workload.coolingDown = passesLeft > 0
	return true
}

// workloadLogAttrs names a workload in a log line, followed by attrs.
func workloadLogAttrs(workload memoryWorkload, attrs ...any) []any {
	if workload.build != nil {
		return append([]any{"build_id", workload.build.BuildID, "user_id", workload.build.OwnerID}, attrs...)
	}
	return append([]any{"container_id", workload.container.ID, "user_id", workload.container.OwnerID}, attrs...)
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

// applyStops queues the plan's container stops and kills its build
// containers, in order, until they cover its shortfall, passing over
// containers with an active operation and workloads it fails to stop. It
// returns the Docker IDs of the workloads it stopped.
func (w *MemoryPressureWorker) applyStops(ctx context.Context, plan memoryBoostPlan) map[string]bool {
	stopped := map[string]bool{}
	if plan.shortfall <= 0 {
		return stopped
	}
	if len(plan.stops) == 0 {
		w.logger.WarnContext(ctx, "user memory pool is short but no workload exceeds its reservation",
			"free_bytes", plan.free, "min_free_bytes", plan.free+plan.shortfall)
		return stopped
	}
	shortfall := plan.shortfall
	for _, stop := range plan.stops {
		if shortfall <= 0 {
			break
		}
		if !w.stop(ctx, stop) {
			continue
		}
		stopped[stop.workload.dockerID()] = true
		// Stopping frees everything the workload takes from the pool, not
		// only its excess.
		shortfall -= stop.workload.poolUse()
	}
	return stopped
}

// stop queues a container's platform stop or kills a build container, which
// Builder then ends as failed for resource exhaustion. It reports whether
// the workload is being stopped.
func (w *MemoryPressureWorker) stop(ctx context.Context, stop memoryStop) bool {
	attrs := func(extra ...any) []any { return workloadLogAttrs(stop.workload, extra...) }
	if stop.workload.build != nil {
		if err := w.docker.KillContainer(ctx, stop.workload.build.DockerID); err != nil {
			w.logger.WarnContext(ctx, "failed to kill build container under memory pressure", attrs("error", err)...)
			return false
		}
		w.logger.WarnContext(ctx, "build container killed due to memory pressure", attrs(
			"working_set_bytes", stop.workload.workingSet, "standard_limit_bytes", stop.workload.standardLimit)...)
		return true
	}
	c := stop.workload.container
	c.Status = model.ContainerStatusRunning
	if err := queueContainerStop(ctx, w.repo, c, stop.reason); err != nil {
		if errors.Is(err, apperrors.ErrConflict) {
			w.logger.DebugContext(ctx, "container to stop already has an active operation", attrs()...)
			return false
		}
		w.logger.WarnContext(ctx, "failed to queue memory pressure stop", attrs("error", err)...)
		return false
	}
	w.logger.WarnContext(ctx, "container stop queued due to memory pressure", attrs(
		"working_set_bytes", stop.workload.workingSet, "reservation_bytes", stop.workload.reservation,
		"standard_limit_bytes", stop.workload.standardLimit)...)
	return true
}

// changeLimit sets a workload's memory limit, unless it is a container with
// an active operation of its own. Swap follows the limit; CPU is left as it
// is, and so is a build's memory reservation. A lowered limit starts the
// cooldown.
func (w *MemoryPressureWorker) changeLimit(ctx context.Context, change memoryLimitChange, cfg config.SystemConfig) {
	workload := change.workload
	update := model.ContainerResourceUpdate{MemoryLimitBytes: change.limit}
	if workload.build != nil {
		update.MemorySwapMultiplier = cfg.BuildMemorySwapMultiplier
	} else {
		c := workload.container
		active, err := w.repo.HasActiveOperation(ctx, model.ResourceTypeContainer, c.ID)
		if err != nil {
			w.logger.WarnContext(ctx, "failed to check container operations before a limit change", workloadLogAttrs(workload, "error", err)...)
			return
		}
		if active {
			w.logger.DebugContext(ctx, "container with an active operation keeps its memory limit", workloadLogAttrs(workload)...)
			return
		}
		update.MemoryReservation = workload.reservation
		update.MemorySwapMultiplier = cfg.ContainerMemorySwapMultiplier
	}
	if err := w.docker.UpdateContainerResources(ctx, workload.dockerID(), update); err != nil {
		w.logger.WarnContext(ctx, "failed to change workload memory limit", workloadLogAttrs(workload, "error", err)...)
		return
	}
	lowered := change.limit < workload.limit
	if lowered {
		w.cooldown[workload.dockerID()] = boostCooldownPasses
	}
	var message string
	switch {
	case workload.build != nil && lowered:
		message = "build memory limit lowered"
	case workload.build != nil:
		message = "build memory limit raised"
	case lowered:
		message = "container memory limit lowered"
	default:
		message = "container memory limit raised"
	}
	w.logger.InfoContext(ctx, message, workloadLogAttrs(workload,
		"working_set_bytes", workload.workingSet,
		"old_limit_bytes", workload.limit, "new_limit_bytes", change.limit,
		"standard_limit_bytes", workload.standardLimit)...)
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
