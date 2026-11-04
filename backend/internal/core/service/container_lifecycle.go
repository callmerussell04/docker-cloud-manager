package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/google/uuid"
)

// lifecycleStep is the Docker work one queued container operation (start,
// stop, delete, expose) contributes. The lifecycle executor owns everything
// around it: claiming and fencing, requeue and failure, recording the result,
// completing the operation and removing unrecorded Docker containers.
type lifecycleStep struct {
	// requiresRuntime rejects a container that has no recorded Docker container.
	requiresRuntime bool
	// validate, when set, rejects a message the step cannot run.
	validate func(msg queue.ContainerLifecycleMessage) error
	// run does the Docker work. It returns the change to record, also on
	// error, so that a container it created is removed.
	run func(ctx context.Context, c model.Container, msg queue.ContainerLifecycleMessage) (lifecycleChange, error)
}

// lifecycleChange is what a step did in Docker and how to record it.
type lifecycleChange struct {
	// created is a Docker container the step created. Until record succeeds
	// its generation is unrecorded, so the executor removes it on failure.
	created string
	// replaced is the Docker container that record supersedes. The executor
	// removes it, best effort, once record succeeds.
	replaced string
	// record writes the result to the container row. On failure the executor
	// restores the previous status and fails the operation.
	record func(ctx context.Context) error
	// status, when set, is written after record. Its failure is returned and
	// leaves the operation running, because the record already points at the
	// new state.
	status string
	// settled runs once the result is recorded: rebalancing, project status,
	// network cleanup and the business log.
	settled func(ctx context.Context)
}

// errRecordedContainerMissing marks a step error meaning the container's
// recorded Docker container no longer exists.
var errRecordedContainerMissing = errors.New("recorded docker container is missing")

// recordedContainerError marks a Docker not-found answer about the recorded
// container as errRecordedContainerMissing.
func recordedContainerError(err error) error {
	if isDockerNotFound(err) {
		return fmt.Errorf("%w: %w", errRecordedContainerMissing, err)
	}
	return err
}

func (s *ContainerService) lifecycleStep(operation string) (lifecycleStep, bool) {
	switch operation {
	case model.OperationStart:
		return lifecycleStep{requiresRuntime: true, run: s.runQueuedStart}, true
	case model.OperationStop:
		return lifecycleStep{requiresRuntime: true, run: s.runQueuedStop}, true
	case model.OperationDelete:
		return lifecycleStep{run: s.runQueuedDelete}, true
	case model.OperationExpose:
		return lifecycleStep{requiresRuntime: true, validate: validateExposeMessage, run: s.runQueuedExpose}, true
	default:
		return lifecycleStep{}, false
	}
}

// executeQueuedLifecycle is the lifecycle executor for queued start, stop,
// delete and expose. Failed writes are returned to the queue consumer, joined
// with the operation's own error.
func (s *ContainerService) executeQueuedLifecycle(ctx context.Context, op model.ResourceOperation, containerID uuid.UUID, msg queue.ContainerLifecycleMessage) error {
	maxAttempts := s.config.Get().ContainerCreateMaxAttempts
	claimed, ok, err := s.repo.ClaimPendingOperation(ctx, op.ID, maxAttempts)
	if err != nil {
		return err
	}
	if !ok {
		if claimed.Status == model.OperationStatusPending && maxAttempts > 0 && claimed.Attempts >= maxAttempts {
			cause := apperrors.New(apperrors.ErrTimeout, "container lifecycle attempts exhausted")
			return s.markLifecycleFailed(ctx, op.ID, containerID, restoredStatus(msg.PreviousStatus), cause)
		}
		return nil
	}
	if claimed.ResourceType != model.ResourceTypeContainer || claimed.ResourceID != containerID {
		cause := apperrors.New(apperrors.ErrBadRequest, "container lifecycle message does not match operation")
		return errors.Join(cause, s.completeQueuedOperation(ctx, s.repo, op.ID, model.OperationStatusFailed, cause))
	}
	step, ok := s.lifecycleStep(claimed.Operation)
	if !ok {
		cause := apperrors.New(apperrors.ErrBadRequest, "unsupported container lifecycle operation")
		return s.failAndRestoreLifecycle(ctx, op.ID, containerID, msg, cause)
	}

	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return errors.Join(err, s.completeQueuedOperation(ctx, s.repo, op.ID, model.OperationStatusFailed, err))
	}
	if step.requiresRuntime && c.DockerID == "" {
		return s.failAndRestoreLifecycle(ctx, op.ID, containerID, msg, resourceUnavailableError("container"))
	}
	if step.validate != nil {
		if err := step.validate(msg); err != nil {
			return s.failAndRestoreLifecycle(ctx, op.ID, containerID, msg, err)
		}
	}
	if err := s.ensureCreateOperationRunning(ctx, s.repo, op.ID); err != nil {
		return err
	}

	change, err := step.run(ctx, c, msg)
	if err != nil {
		s.removeLifecycleContainer(ctx, containerID, change.created)
		return s.settleLifecycleStepError(ctx, op.ID, containerID, msg, err)
	}
	if err := change.record(ctx); err != nil {
		s.removeLifecycleContainer(ctx, containerID, change.created)
		return s.failAndRestoreLifecycle(ctx, op.ID, containerID, msg, err)
	}
	s.removeLifecycleContainer(ctx, containerID, change.replaced)
	if change.status != "" {
		if err := s.repo.UpdateStatus(ctx, containerID, change.status); err != nil {
			return fmt.Errorf("record container status: %w", err)
		}
	}
	completeErr := s.completeQueuedOperation(ctx, s.repo, op.ID, model.OperationStatusDone, nil)
	if change.settled != nil {
		change.settled(ctx)
	}
	return completeErr
}

// settleLifecycleStepError requeues a retryable Docker failure, marks a
// container whose Docker container is gone as missing, and fails the
// operation otherwise.
func (s *ContainerService) settleLifecycleStepError(ctx context.Context, operationID, containerID uuid.UUID, msg queue.ContainerLifecycleMessage, err error) error {
	if errors.Is(err, errRecordedContainerMissing) {
		return errors.Join(resourceUnavailableError("container"), s.markLifecycleFailed(ctx, operationID, containerID, model.ContainerStatusMissing, resourceMissingError("container")))
	}
	err = normalizeContainerRuntimeError(err)
	if !errors.Is(err, apperrors.ErrTimeout) && !errors.Is(err, apperrors.ErrUnavailable) {
		return s.failAndRestoreLifecycle(ctx, operationID, containerID, msg, err)
	}
	requeueErr := s.repo.RequeueOperation(context.WithoutCancel(ctx), operationID, err)
	if requeueErr == nil {
		return err
	}
	return errors.Join(fmt.Errorf("requeue container operation: %w", requeueErr), s.failAndRestoreLifecycle(ctx, operationID, containerID, msg, err))
}

// failAndRestoreLifecycle fails the operation with cause and restores the container's
// previous status.
func (s *ContainerService) failAndRestoreLifecycle(ctx context.Context, operationID, containerID uuid.UUID, msg queue.ContainerLifecycleMessage, cause error) error {
	return errors.Join(cause, s.markLifecycleFailed(ctx, operationID, containerID, restoredStatus(msg.PreviousStatus), cause))
}

// markLifecycleFailed marks the container with status and cause and fails the
// operation, returning the writes that failed. A container or operation that
// is already gone is not a failure.
func (s *ContainerService) markLifecycleFailed(ctx context.Context, operationID, containerID uuid.UUID, status string, cause error) error {
	writeCtx, cancel := detachedContainerStateContext(ctx)
	defer cancel()
	cause = normalizeContainerRuntimeError(cause)
	var markErr, completeErr error
	if err := s.repo.MarkStatusError(writeCtx, containerID, status, cause); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		markErr = fmt.Errorf("record failed container status %s: %w", status, err)
	}
	if err := s.repo.CompleteOperation(writeCtx, operationID, model.OperationStatusFailed, cause); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		completeErr = fmt.Errorf("record container operation failed status: %w", err)
	}
	return errors.Join(markErr, completeErr)
}

// restoredStatus is the status a failed operation returns the container to:
// its status before the request, or error when that was itself transitional.
func restoredStatus(previous string) string {
	switch previous {
	case "", model.ContainerStatusPending, model.ContainerStatusCreating, model.ContainerStatusStarting, model.ContainerStatusStopping, model.ContainerStatusExposing, model.ContainerStatusDeleting:
		return model.ContainerStatusError
	default:
		return previous
	}
}

// removeLifecycleContainer removes a Docker container whose generation is not
// recorded. Removal is best effort; the operation's outcome stands either way.
func (s *ContainerService) removeLifecycleContainer(ctx context.Context, containerID uuid.UUID, dockerID string) {
	if dockerID == "" {
		return
	}
	cleanupCtx, cancel := detachedCleanupContext(ctx)
	defer cancel()
	if err := s.dockerAPI.RemoveContainer(cleanupCtx, dockerID, true); err != nil && !isDockerNotFound(err) {
		s.logger.WarnContext(cleanupCtx, "failed to remove unrecorded container generation", "container_id", containerID, "docker_id", dockerID, "error", normalizeContainerRuntimeError(err))
	}
}

func (s *ContainerService) runQueuedStart(ctx context.Context, c model.Container, _ queue.ContainerLifecycleMessage) (lifecycleChange, error) {
	if err := s.dockerAPI.StartContainer(ctx, c.DockerID); err != nil {
		return lifecycleChange{}, recordedContainerError(err)
	}
	var ttlDeadline *time.Time
	if ttlHours := s.config.Get().ContainerTTLHours; ttlHours > 0 {
		deadline := time.Now().Add(time.Duration(ttlHours) * time.Hour)
		ttlDeadline = &deadline
	}
	return lifecycleChange{
		record: func(ctx context.Context) error {
			return s.repo.UpdateStatusAndTTLDeadline(ctx, c.ID, model.ContainerStatusRunning, ttlDeadline)
		},
		settled: func(ctx context.Context) {
			s.logger.InfoContext(ctx, "container started", "container_id", c.ID, "owner_id", c.OwnerID)
			s.RequestRebalance()
			s.refreshProjectStatus(ctx, c.ProjectID)
		},
	}, nil
}

func (s *ContainerService) runQueuedStop(ctx context.Context, c model.Container, _ queue.ContainerLifecycleMessage) (lifecycleChange, error) {
	if err := s.dockerAPI.StopContainer(ctx, c.DockerID, s.config.Get().ContainerStopTimeout); err != nil {
		return lifecycleChange{}, recordedContainerError(err)
	}
	return lifecycleChange{
		record: func(ctx context.Context) error {
			return s.repo.UpdateStatus(ctx, c.ID, model.ContainerStatusExited)
		},
		settled: func(ctx context.Context) {
			s.logger.InfoContext(ctx, "container stopped", "container_id", c.ID, "owner_id", c.OwnerID)
			s.RequestRebalance()
			s.refreshProjectStatus(ctx, c.ProjectID)
		},
	}, nil
}

// runQueuedDelete removes the Docker container. One Docker no longer holds is
// already deleted.
func (s *ContainerService) runQueuedDelete(ctx context.Context, c model.Container, _ queue.ContainerLifecycleMessage) (lifecycleChange, error) {
	if c.DockerID != "" {
		if err := s.dockerAPI.RemoveContainer(ctx, c.DockerID, true); err != nil && !isDockerNotFound(err) {
			return lifecycleChange{}, err
		}
	}
	return lifecycleChange{
		record: func(ctx context.Context) error {
			return s.repo.Delete(ctx, c.ID)
		},
		settled: func(ctx context.Context) {
			s.cleanupUserNetworkIfUnused(ctx, c.OwnerID)
			s.refreshProjectStatus(ctx, c.ProjectID)
			s.logger.InfoContext(ctx, "container deleted", "container_id", c.ID, "owner_id", c.OwnerID)
		},
	}, nil
}

func validateExposeMessage(msg queue.ContainerLifecycleMessage) error {
	if msg.DomainPrefix == "" || msg.InternalPort <= 0 {
		return apperrors.New(apperrors.ErrBadRequest, "domain prefix and internal port are required")
	}
	return nil
}

// runQueuedExpose replaces the Docker container with a new generation that
// carries the new routing, started when the container was running.
func (s *ContainerService) runQueuedExpose(ctx context.Context, c model.Container, msg queue.ContainerLifecycleMessage) (lifecycleChange, error) {
	inspect, err := s.dockerAPI.InspectContainer(ctx, c.DockerID)
	if err != nil {
		return lifecycleChange{}, recordedContainerError(err)
	}
	nextGeneration := max(c.DockerGeneration+1, 2)
	newDockerID, err := s.dockerAPI.CreateContainer(ctx, s.exposeRuntimeSpec(c, inspect, msg, nextGeneration))
	if err != nil {
		return lifecycleChange{}, err
	}
	targetStatus := msg.PreviousStatus
	if targetStatus == "" || targetStatus == model.ContainerStatusExposing {
		targetStatus = model.ContainerStatusCreated
	}
	change := lifecycleChange{
		created:  newDockerID,
		replaced: c.DockerID,
		record: func(ctx context.Context) error {
			return s.repo.UpdateDockerIDRoutingAndGeneration(ctx, c.ID, newDockerID, msg.DomainPrefix, msg.InternalPort, nextGeneration)
		},
		status: targetStatus,
		settled: func(ctx context.Context) {
			s.refreshProjectStatus(ctx, c.ProjectID)
			s.logger.InfoContext(ctx, "container exposed", "container_id", c.ID, "owner_id", c.OwnerID, "domain_prefix", msg.DomainPrefix, "internal_port", msg.InternalPort)
		},
	}
	if msg.PreviousStatus == model.ContainerStatusRunning || c.DesiredStatus == model.ContainerStatusRunning {
		if err := s.dockerAPI.StartContainer(ctx, newDockerID); err != nil {
			return change, err
		}
	}
	return change, nil
}

// exposeRuntimeSpec is the inspected container's runtime with the new
// generation's name and routing.
func (s *ContainerService) exposeRuntimeSpec(c model.Container, inspect model.ContainerInspection, msg queue.ContainerLifecycleMessage, generation int) model.ContainerRuntimeSpec {
	cfg := s.config.Get()
	networkAlias := c.NetworkAlias
	if networkAlias == "" {
		networkAlias = c.Name
	}
	spec := model.ContainerRuntimeSpec{
		ContainerID:          c.ID.String(),
		OwnerID:              c.OwnerID.String(),
		Generation:           generation,
		ContainerName:        fmt.Sprintf("%s_g%d", strings.TrimPrefix(inspect.Name, "/"), generation),
		NetworkAlias:         networkAlias,
		ImageName:            inspect.Image,
		NetworkName:          userNetworkName(c.OwnerID),
		Domain:               fmt.Sprintf("%s.%s", msg.DomainPrefix, cfg.BaseDomain),
		InternalPort:         msg.InternalPort,
		EnvVars:              append([]string(nil), inspect.Env...),
		MemoryLimitBytes:     inspect.MemoryLimitBytes,
		MemoryReservation:    inspect.MemoryReservation,
		MemorySwapMultiplier: cfg.ContainerMemorySwapMultiplier,
		CPUShares:            inspect.CPUShares,
		CPUQuota:             inspect.CPUQuota,
		CPUPeriod:            inspect.CPUPeriod,
		PidsLimit:            cfg.ContainerPidsLimit,
		ProxyNetworkName:     cfg.ProxyNetworkName,
		VolumeMounts:         append([]model.ContainerMountSpec(nil), inspect.Mounts...),
		MaxLogSize:           cfg.MaxLogSize,
		MaxLogFiles:          cfg.MaxLogFiles,
		StorageQuota:         cfg.ContainerDiskQuota,
		Command:              inspect.Command,
		Entrypoint:           inspect.Entrypoint,
		Restart:              inspect.Restart,
		Healthcheck:          inspect.Healthcheck,
	}
	if c.ProjectID != nil {
		spec.ProjectID = c.ProjectID.String()
	}
	return spec
}
