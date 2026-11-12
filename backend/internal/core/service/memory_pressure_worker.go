package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"sync/atomic"
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
	InspectContainer(ctx context.Context, dockerID string) (model.ContainerInspection, error)
	UpdateContainerResources(ctx context.Context, dockerID string, resources model.ContainerResourceUpdate) error
	ListRunningBuildContainers(ctx context.Context) ([]model.BuildContainer, error)
	KillContainer(ctx context.Context, dockerID string) error
}

type MemoryPressureHostMetrics interface {
	GetTotalMemory() (int64, error)
	// GetCPULoad is the host's CPU use in percent. It blocks for about a
	// second while it measures.
	GetCPULoad() (float64, error)
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
// A limit above what the settings now allow, after the boost ceiling was
// lowered or boost turned off, is lowered the same way on any pass.
// When less than the stop floor is free, the worker stops containers and
// kills builds using borrowed memory, then those using more than they
// reserved, largest first. It never stops a container within its
// reservation or a build within its build memory. This worker is the only
// thing that lowers a running workload's memory limit.
//
// With CPU boost on, the same pass raises the CPU quota of a workload using
// nearly all of it while host CPU is idle, up to its CPU boost ceiling, and
// returns raised quotas to standard when the host is busy. Taking CPU back
// only throttles, so it needs no usage check. A quota that was returned is
// not raised again within the cooldown.
type MemoryPressureWorker struct {
	repo    MemoryPressureContainerRepo
	docker  MemoryPressureDockerAPI
	metrics MemoryPressureHostMetrics
	builds  activeSystemBuildCounter
	quotas  UserQuotaProvider
	cfg     MemoryPressureConfigProvider
	logger  *slog.Logger

	// mu serializes passes; cooldown and cpuCooldown are the passes left, by
	// Docker ID, before a lowered memory limit or CPU quota may be raised
	// again, and cpuSamples the last CPU time read of each workload.
	mu          sync.Mutex
	cooldown    map[string]int
	cpuCooldown map[string]int
	cpuSamples  map[string]cpuSample
	// borrowed is the memory the last pass measured above standard limits.
	borrowed atomic.Int64
}

// cpuSample is a workload's cumulative CPU time and when it was read.
type cpuSample struct {
	nanoseconds int64
	at          time.Time
}

type MemoryPressureWorkerDeps struct {
	Repository MemoryPressureContainerRepo
	Docker     MemoryPressureDockerAPI
	Metrics    MemoryPressureHostMetrics
	Builds     activeSystemBuildCounter
	Quotas     UserQuotaProvider
	Config     MemoryPressureConfigProvider
	Logger     *slog.Logger
}

func NewMemoryPressureWorker(deps MemoryPressureWorkerDeps) (*MemoryPressureWorker, error) {
	required := []struct {
		name  string
		value any
	}{
		{"repository", deps.Repository}, {"docker", deps.Docker}, {"metrics", deps.Metrics},
		{"builds", deps.Builds}, {"quotas", deps.Quotas}, {"config", deps.Config},
	}
	for _, dependency := range required {
		if dependency.value == nil || isNilServiceDependency(dependency.value) {
			return nil, fmt.Errorf("memory pressure worker: %s is required", dependency.name)
		}
	}
	return &MemoryPressureWorker{
		repo:        deps.Repository,
		docker:      deps.Docker,
		metrics:     deps.Metrics,
		builds:      deps.Builds,
		quotas:      deps.Quotas,
		cfg:         deps.Config,
		logger:      logging.WithComponent(deps.Logger, "memory_pressure_worker"),
		cooldown:    map[string]int{},
		cpuCooldown: map[string]int{},
		cpuSamples:  map[string]cpuSample{},
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
// decides with planMemoryBoost and planCPUBoost, and applies the stops and
// the limit and quota changes, at most one update per workload. Lowered
// memory limits go out before the CPU decision, which may wait a second on
// host load, so they rest on fresh measurements; such a workload gets no CPU
// change in the same pass. A zero MemoryPressureMinFreeBytes turns the
// worker off. A workload whose
// usage cannot be read counts as using nothing and is left alone.
func (w *MemoryPressureWorker) RelievePressure(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	cfg := w.cfg.Get()
	if cfg.MemoryPressureMinFreeBytes <= 0 {
		w.borrowed.Store(0)
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
	next := passState{cooldown: map[string]int{}, cpuCooldown: map[string]int{}, cpuSamples: map[string]cpuSample{}}
	for _, c := range running {
		if c.DockerID == "" {
			continue
		}
		standard := containerResources(c, cfg)
		workload := boostWorkload{
			container:     c,
			reservation:   standard.MemoryReservation,
			standardLimit: standard.MemoryLimitBytes,
			ceiling:       memoryBoostCeiling(c, cfg),

			cpuStandardQuota: standard.CPUQuota,
			cpuCeiling:       cpuBoostCeiling(c, cfg),
			cpuPeriod:        cfg.ContainerCPUPeriod,
		}
		if !w.measure(ctx, &workload, next) {
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
		workload := boostWorkload{
			build:         &b,
			reservation:   cfg.BuildMemoryBytes,
			standardLimit: cfg.BuildMemoryBytes,
			ceiling:       cfg.BuildMemoryBytes * cfg.MemoryBoostMultiplier,

			cpuStandardQuota: cfg.BuildCPUQuota,
			cpuCeiling:       cfg.BuildCPUQuota * cfg.CPUBoostMultiplier,
			cpuPeriod:        cfg.BuildCPUPeriod,
		}
		if !w.measure(ctx, &workload, next) {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		in.workloads = append(in.workloads, workload)
	}
	w.cooldown, w.cpuCooldown, w.cpuSamples = next.cooldown, next.cpuCooldown, next.cpuSamples
	if in.quotas, err = w.memoryQuotas(ctx, in.growCandidateOwners()); err != nil {
		return
	}

	plan := planMemoryBoost(in)
	// Stops and lowered limits come first: the CPU decision may wait a
	// second on host load, and a limit must not be lowered on a stale
	// working set. settled workloads get no further change this pass.
	stopped := w.applyStops(ctx, plan)
	settled := maps.Clone(stopped)
	applied, raises := map[string]int64{}, map[string]int64{}
	for _, change := range plan.changes {
		id := change.workload.dockerID()
		switch {
		case stopped[id]:
		case change.limit >= change.workload.limit:
			raises[id] = change.limit
		default:
			settled[id] = true
			if w.changeResources(ctx, change.workload, change.limit, cpuQuotaChange{}, cfg) {
				applied[id] = change.limit
			}
		}
	}
	cpuChanges := w.planCPU(ctx, cfg, in.workloads, settled)
	if ctx.Err() != nil {
		return
	}
	quotas := map[string]cpuQuotaChange{}
	for _, change := range cpuChanges {
		quotas[change.workload.dockerID()] = change
	}
	for _, workload := range in.workloads {
		id := workload.dockerID()
		if settled[id] || (raises[id] == 0 && quotas[id].quota == 0) {
			continue
		}
		// The CPU change carries what inspection read; the memory change
		// does not.
		if change, ok := quotas[id]; ok {
			workload = change.workload
		}
		if w.changeResources(ctx, workload, raises[id], quotas[id], cfg) && raises[id] != 0 {
			applied[id] = raises[id]
		}
	}
	w.borrowed.Store(borrowedAfter(in, stopped, applied))
}

// borrowedAfter is what the running workloads borrow once the pass's
// stops and applied limits take effect.
func borrowedAfter(in memoryBoostInput, stopped map[string]bool, applied map[string]int64) int64 {
	var total int64
	for _, workload := range in.workloads {
		id := workload.dockerID()
		if stopped[id] {
			continue
		}
		if limit, ok := applied[id]; ok && limit != 0 {
			workload.limit = limit
		}
		total += workload.borrowed()
	}
	return total
}

// BorrowedMemoryBytes is the memory running workloads had borrowed above
// their standard limits when the last pass measured them: 0 before the first
// pass and while the worker is off. With memory boost off it is what
// workloads still hold of limits raised earlier.
func (w *MemoryPressureWorker) BorrowedMemoryBytes() int64 {
	return w.borrowed.Load()
}

// passState is what the worker carries from one pass to the next.
type passState struct {
	cooldown    map[string]int
	cpuCooldown map[string]int
	cpuSamples  map[string]cpuSample
}

// planCPU decides CPU boost when it is on. Only workloads close to their
// standard CPU quota can change, so host CPU load, which takes about a
// second to read, is read only when there is one, and their quotas are
// inspected only when the load is low or high enough to act on. Settled
// workloads, stopped or given a lower memory limit this pass, are left out.
func (w *MemoryPressureWorker) planCPU(ctx context.Context, cfg config.SystemConfig, workloads []boostWorkload, settled map[string]bool) []cpuQuotaChange {
	if cfg.CPUBoostMultiplier <= cfg.MaxCPUBurstMultiplier {
		return nil
	}
	var near []boostWorkload
	for _, workload := range workloads {
		if !settled[workload.dockerID()] && workload.nearCPUQuota(workload.cpuStandardQuota) {
			near = append(near, workload)
		}
	}
	if len(near) == 0 {
		return nil
	}
	load, err := w.metrics.GetCPULoad()
	if err != nil {
		w.logger.WarnContext(ctx, "failed to read host CPU load", "error", err)
		return nil
	}
	if !cpuBoostActs(load) {
		return nil
	}
	inspected := make([]boostWorkload, 0, len(near))
	for _, workload := range near {
		info, err := w.docker.InspectContainer(ctx, workload.dockerID())
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.logger.WarnContext(ctx, "failed to read workload CPU quota", workloadLogAttrs(workload, "error", err)...)
			continue
		}
		workload.cpuQuota, workload.cpuQuotaPeriod = info.CPUQuota, info.CPUPeriod
		inspected = append(inspected, workload)
	}
	return planCPUBoost(load, inspected)
}

// measure reads a workload's working set, current limit and CPU use since
// the previous pass, and carries its cooldowns and CPU sample into next. It
// reports false when the usage cannot be read.
func (w *MemoryPressureWorker) measure(ctx context.Context, workload *boostWorkload, next passState) bool {
	dockerID := workload.dockerID()
	passesLeft := w.cooldown[dockerID]
	if passesLeft > 1 {
		next.cooldown[dockerID] = passesLeft - 1
	}
	cpuPassesLeft := w.cpuCooldown[dockerID]
	if cpuPassesLeft > 1 {
		next.cpuCooldown[dockerID] = cpuPassesLeft - 1
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
	workload.cpuCoolingDown = cpuPassesLeft > 0
	sample := cpuSample{nanoseconds: usage.CPUUsageNanoseconds, at: time.Now()}
	next.cpuSamples[dockerID] = sample
	// A restarted container's CPU time starts again from zero.
	if previous, ok := w.cpuSamples[dockerID]; ok && sample.at.After(previous.at) && sample.nanoseconds >= previous.nanoseconds {
		workload.cpuUsed = float64(sample.nanoseconds-previous.nanoseconds) / float64(sample.at.Sub(previous.at).Nanoseconds())
		workload.cpuMeasured = true
	}
	return true
}

// workloadLogAttrs names a workload in a log line, followed by attrs.
func workloadLogAttrs(workload boostWorkload, attrs ...any) []any {
	if workload.build != nil {
		return append([]any{"build_id", workload.build.BuildID, "user_id", workload.build.OwnerID}, attrs...)
	}
	return append([]any{"container_id", workload.container.ID, "user_id", workload.container.OwnerID}, attrs...)
}

// memoryQuotas reads the memory quota of each owner. An owner whose quota
// cannot be read gets none, so nothing is lent to them this pass. It fails
// only when ctx is done.
func (w *MemoryPressureWorker) memoryQuotas(ctx context.Context, owners []uuid.UUID) (map[uuid.UUID]int64, error) {
	quotaBytes := make(map[uuid.UUID]int64, len(owners))
	for _, owner := range owners {
		quota, err := w.quotas.EffectiveUserQuota(ctx, owner)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			w.logger.WarnContext(ctx, "failed to read user memory quota", "user_id", owner, "error", err)
			continue
		}
		quotaBytes[owner] = quota.RAMMB * bytesPerMB
	}
	return quotaBytes, nil
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

// changeResources sends a workload's new memory limit, CPU quota or both
// in one update, unless it is a container with an active operation of its
// own. A zero limit or quota leaves that resource as it is; the CPU period
// is sent only with a quota that changes it. Swap follows the
// limit; CPU shares and period, and a build's memory reservation, are left
// as they are. A lowered memory limit or CPU quota starts its cooldown. It
// reports whether the update was sent.
func (w *MemoryPressureWorker) changeResources(ctx context.Context, workload boostWorkload, limit int64, cpu cpuQuotaChange, cfg config.SystemConfig) bool {
	quota := cpu.quota
	update := model.ContainerResourceUpdate{MemoryLimitBytes: limit, CPUQuota: quota, CPUPeriod: cpu.period}
	kind := "build"
	if workload.build == nil {
		kind = "container"
		c := workload.container
		active, err := w.repo.HasActiveOperation(ctx, model.ResourceTypeContainer, c.ID)
		if err != nil {
			w.logger.WarnContext(ctx, "failed to check container operations before a resource change", workloadLogAttrs(workload, "error", err)...)
			return false
		}
		if active {
			w.logger.DebugContext(ctx, "container with an active operation keeps its resources", workloadLogAttrs(workload)...)
			return false
		}
	}
	if limit != 0 {
		update.MemorySwapMultiplier = cfg.BuildMemorySwapMultiplier
		if workload.build == nil {
			update.MemoryReservation = workload.reservation
			update.MemorySwapMultiplier = cfg.ContainerMemorySwapMultiplier
		}
	}
	if err := w.docker.UpdateContainerResources(ctx, workload.dockerID(), update); err != nil {
		w.logger.WarnContext(ctx, "failed to change workload resources", workloadLogAttrs(workload, "error", err)...)
		return false
	}
	if limit != 0 {
		direction := "raised"
		if limit < workload.limit {
			direction = "lowered"
			w.cooldown[workload.dockerID()] = boostCooldownPasses
		}
		w.logger.InfoContext(ctx, kind+" memory limit "+direction, workloadLogAttrs(workload,
			"working_set_bytes", workload.workingSet,
			"old_limit_bytes", workload.limit, "new_limit_bytes", limit,
			"standard_limit_bytes", workload.standardLimit)...)
	}
	if quota != 0 {
		direction := "raised"
		if quota*workload.cpuQuotaPeriod < workload.cpuQuota*workload.cpuPeriod {
			direction = "lowered"
			w.cpuCooldown[workload.dockerID()] = boostCooldownPasses
		}
		w.logger.InfoContext(ctx, kind+" cpu quota "+direction, workloadLogAttrs(workload,
			"cpu_used_cores", workload.cpuUsed,
			"old_cpu_quota", workload.cpuQuota, "old_cpu_period", workload.cpuQuotaPeriod,
			"new_cpu_quota", quota, "standard_cpu_quota", workload.cpuStandardQuota, "cpu_period", workload.cpuPeriod)...)
	}
	return true
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
