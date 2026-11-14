package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
)

const (
	// retentionPeriod is how long a finished queued operation, a spent
	// Registry build credential and a released staged-object reservation are
	// kept for debugging before they are deleted.
	retentionPeriod = 7 * 24 * time.Hour
	// retentionInterval is how often expired rows are deleted.
	retentionInterval = time.Hour
	// retentionStartDelay is how soon after start the first deletion runs.
	retentionStartDelay = time.Minute
	// retentionBatchSize bounds the rows one statement deletes.
	retentionBatchSize = 1000
)

// RetentionStore deletes at most limit rows that ended before cutoff and
// reports how many it deleted. It never deletes a row of work in progress.
type RetentionStore interface {
	DeleteFinishedOperations(ctx context.Context, cutoff time.Time, limit int) (int64, error)
	DeleteSpentBuildCredentials(ctx context.Context, cutoff time.Time, limit int) (int64, error)
	DeleteReleasedStagedObjects(ctx context.Context, cutoff time.Time, limit int) (int64, error)
}

// RetentionWorker deletes rows older than the retention period, shortly after
// start and then every hour.
type RetentionWorker struct {
	store  RetentionStore
	logger *slog.Logger
}

func NewRetentionWorker(store RetentionStore, logger *slog.Logger) *RetentionWorker {
	return &RetentionWorker{store: store, logger: logging.WithComponent(logger, "retention_worker")}
}

func (w *RetentionWorker) Run(ctx context.Context) {
	next := retentionStartDelay
	runPeriodically(ctx, func() int64 {
		seconds := int64(next / time.Second)
		next = retentionInterval
		return seconds
	}, w.deleteExpired)
}

func (w *RetentionWorker) deleteExpired(ctx context.Context) {
	cutoff := time.Now().Add(-retentionPeriod)
	operations := w.deleteAll(ctx, "finished operations", cutoff, w.store.DeleteFinishedOperations)
	credentials := w.deleteAll(ctx, "spent build credentials", cutoff, w.store.DeleteSpentBuildCredentials)
	objects := w.deleteAll(ctx, "released staged objects", cutoff, w.store.DeleteReleasedStagedObjects)
	if operations+credentials+objects > 0 {
		w.logger.InfoContext(ctx, "retention deleted expired rows",
			"operations", operations, "build_credentials", credentials, "staged_objects", objects)
	}
}

// deleteAll deletes in batches until a batch comes back short. A failure is
// logged and leaves the rest for the next run.
func (w *RetentionWorker) deleteAll(ctx context.Context, kind string, cutoff time.Time, deleteBatch func(context.Context, time.Time, int) (int64, error)) int64 {
	var total int64
	for ctx.Err() == nil {
		deleted, err := deleteBatch(ctx, cutoff, retentionBatchSize)
		total += deleted
		if err != nil {
			w.logger.ErrorContext(ctx, "retention delete failed", "kind", kind, "error", err)
			break
		}
		if deleted < retentionBatchSize {
			break
		}
	}
	return total
}
