package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/nilcheck"
)

const (
	// operationRecoveryInterval is how often the operation recovery pass runs;
	// the first pass follows one interval after start.
	operationRecoveryInterval = time.Minute
	// operationExpiryHeadroom is added to the handler deadline before a
	// running operation counts as expired, covering the detached write
	// budgets a worker may still spend after its deadline.
	operationExpiryHeadroom = 5 * time.Minute
)

// The recovery causes are fixed, safe messages that name no value.
var (
	errExpiredOperationRequeued = errors.New("operation expired and was returned to the queue")
	errExpiredOperationFailed   = errors.New("operation expired before it finished")
	errOperationResourceGone    = errors.New("operation resource no longer exists")
)

// ContainerOperationRecoveryStore holds the container recovery statements
// startup recovery and the operation recovery pass share.
type ContainerOperationRecoveryStore interface {
	RecoverInterruptedContainerCreates(ctx context.Context, cause error, scope model.OperationRecoveryScope) ([]model.RecoveredOperation, error)
	FailInterruptedNonResumableOperations(ctx context.Context, cause error, runningLongerThan time.Duration) ([]model.RecoveredOperation, error)
	RecoverLostContainerDeliveries(ctx context.Context, cause error, olderThan time.Duration) (model.LostDeliveryRecovery, error)
}

// ResourceOperationRecoveryStore holds the volume and image recovery
// statements startup recovery and the operation recovery pass share.
type ResourceOperationRecoveryStore interface {
	RecoverInterruptedOperations(ctx context.Context, cause error, scope model.OperationRecoveryScope) ([]model.RecoveredOperation, error)
	RecoverLostResourceDeliveries(ctx context.Context, cause error, olderThan time.Duration) (model.LostDeliveryRecovery, error)
}

// ComposeDeliveryRecoveryStore republishes queued Compose deployments whose
// message was lost.
type ComposeDeliveryRecoveryStore interface {
	RepublishLostComposeDeliveries(ctx context.Context, olderThan time.Duration) ([]model.RecoveredDeployment, error)
}

// OperationRecoveryConfigProvider supplies the handler deadline.
type OperationRecoveryConfigProvider interface {
	Get() config.SystemConfig
}

type OperationRecoveryWorkerDeps struct {
	Containers  ContainerOperationRecoveryStore
	Resources   ResourceOperationRecoveryStore
	Deployments ComposeDeliveryRecoveryStore
	Config      OperationRecoveryConfigProvider
	Logger      *slog.Logger
}

// OperationRecoveryWorker settles queued work that would otherwise stay stuck
// until a Core restart: expired operations, by startup recovery's rules, and
// lost deliveries of operations and queued Compose deployments, by
// publishing them again.
type OperationRecoveryWorker struct {
	containers  ContainerOperationRecoveryStore
	resources   ResourceOperationRecoveryStore
	deployments ComposeDeliveryRecoveryStore
	cfg         OperationRecoveryConfigProvider
	logger      *slog.Logger
}

func NewOperationRecoveryWorker(deps OperationRecoveryWorkerDeps) (*OperationRecoveryWorker, error) {
	required := []struct {
		name  string
		value any
	}{
		{"containers", deps.Containers}, {"resources", deps.Resources}, {"deployments", deps.Deployments}, {"config", deps.Config},
	}
	for _, dependency := range required {
		if nilcheck.IsNil(dependency.value) {
			return nil, fmt.Errorf("operation recovery worker: %s is required", dependency.name)
		}
	}
	return &OperationRecoveryWorker{
		containers:  deps.Containers,
		resources:   deps.Resources,
		deployments: deps.Deployments,
		cfg:         deps.Config,
		logger:      logging.WithComponent(deps.Logger, "operation_recovery_worker"),
	}, nil
}

// Run runs a recovery pass every interval until ctx ends.
func (w *OperationRecoveryWorker) Run(ctx context.Context) {
	runPeriodically(ctx, func() int64 { return int64(operationRecoveryInterval / time.Second) }, w.RunPass)
}

// recoveryCounts counts one pass's outcomes for its summary line.
type recoveryCounts struct {
	requeued, failed, republished, ended, republishedDeployments int
}

func (c recoveryCounts) total() int {
	return c.requeued + c.failed + c.republished + c.ended + c.republishedDeployments
}

// RunPass runs one recovery pass. Each statement is independent: a failed one is
// logged and retried on the next pass, and the others still run.
func (w *OperationRecoveryWorker) RunPass(ctx context.Context) {
	expiredAfter := handlerDeadline(w.cfg.Get()) + operationExpiryHeadroom
	expired := model.OperationRecoveryScope{RunningLongerThan: expiredAfter}
	// A message counts as lost only after the same bound. A delivery can wait
	// in the queue behind long operations (two workers per queue by default,
	// each operation up to the handler deadline), and a shorter bound would
	// publish a duplicate of such a message on every pass. Losses are rare: a
	// requeued message is never dead-lettered, only one abandoned by twenty
	// closed channels, so waiting this long costs little.
	lostAfter := expiredAfter
	var counts recoveryCounts

	if ops, err := w.resources.RecoverInterruptedOperations(ctx, errExpiredOperationRequeued, expired); w.ok(ctx, "requeue expired volume and image operations", err) {
		counts.requeued += w.logRecovered(ctx, ops, "requeued")
	}
	if ops, err := w.containers.RecoverInterruptedContainerCreates(ctx, errExpiredOperationRequeued, expired); w.ok(ctx, "requeue expired container creates", err) {
		counts.requeued += w.logRecovered(ctx, ops, "requeued")
	}
	if ops, err := w.containers.FailInterruptedNonResumableOperations(ctx, errExpiredOperationFailed, expiredAfter); w.ok(ctx, "fail expired operations", err) {
		counts.failed += w.logRecovered(ctx, ops, "failed")
	}
	if lost, err := w.containers.RecoverLostContainerDeliveries(ctx, errOperationResourceGone, lostAfter); w.ok(ctx, "recover lost container deliveries", err) {
		counts.republished += w.logRecovered(ctx, lost.Republished, "republished")
		counts.ended += w.logRecovered(ctx, lost.Ended, "ended")
	}
	if lost, err := w.resources.RecoverLostResourceDeliveries(ctx, errOperationResourceGone, lostAfter); w.ok(ctx, "recover lost volume and image deliveries", err) {
		counts.republished += w.logRecovered(ctx, lost.Republished, "republished")
		counts.ended += w.logRecovered(ctx, lost.Ended, "ended")
	}
	if deployments, err := w.deployments.RepublishLostComposeDeliveries(ctx, lostAfter); w.ok(ctx, "republish lost compose deliveries", err) {
		for _, d := range deployments {
			w.logger.WarnContext(ctx, "republished lost compose deployment delivery", "job_id", d.JobID, "project_id", d.ProjectID)
		}
		counts.republishedDeployments += len(deployments)
	}

	if counts.total() > 0 {
		w.logger.InfoContext(ctx, "operation recovery pass settled operations",
			"requeued", counts.requeued, "failed", counts.failed, "republished", counts.republished,
			"ended", counts.ended, "republished_deployments", counts.republishedDeployments)
	}
}

// ok logs a failed recovery statement and reports whether it succeeded.
func (w *OperationRecoveryWorker) ok(ctx context.Context, step string, err error) bool {
	if err != nil {
		w.logger.ErrorContext(ctx, "operation recovery statement failed", "step", step, "error", err)
		return false
	}
	return true
}

// logRecovered logs each recovered operation with its outcome and returns how
// many there were.
func (w *OperationRecoveryWorker) logRecovered(ctx context.Context, ops []model.RecoveredOperation, outcome string) int {
	for _, op := range ops {
		w.logger.WarnContext(ctx, "recovered queued operation",
			"operation_id", op.ID, "resource_type", op.ResourceType, "resource_id", op.ResourceID,
			"operation", op.Operation, "outcome", outcome)
	}
	return len(ops)
}
