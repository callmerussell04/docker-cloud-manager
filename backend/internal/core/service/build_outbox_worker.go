package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/google/uuid"
)

type BuildOutboxRepository interface {
	LeasePendingBuildOutbox(ctx context.Context, limit, maxReleasedPerOwner int) ([]model.BuildQueueOutbox, error)
	GetByID(ctx context.Context, id uuid.UUID) (model.Build, error)
	MarkBuildOutboxPublished(ctx context.Context, id uuid.UUID) error
	MarkBuildOutboxPending(ctx context.Context, id uuid.UUID, cause error) error
	MarkBuildOutboxDiscarded(ctx context.Context, id uuid.UUID, cause error) error
}

type BuildOutboxConfigProvider interface {
	Get() config.SystemConfig
}

type BuildOutboxWorker struct {
	repo   BuildOutboxRepository
	cfg    BuildOutboxConfigProvider
	relay  outboxRelay
	logger *slog.Logger
}

func NewBuildOutboxWorker(repo BuildOutboxRepository, publisher OutboxPublisher, cfg BuildOutboxConfigProvider, logger *slog.Logger) *BuildOutboxWorker {
	logger = logging.WithComponent(logger, "build_outbox_worker")
	return &BuildOutboxWorker{
		repo:   repo,
		cfg:    cfg,
		relay:  newOutboxRelay("build", publisher, logger),
		logger: logger,
	}
}

func (w *BuildOutboxWorker) Run(ctx context.Context) {
	w.relay.run(ctx, func() int64 { return w.cfg.Get().BuildOutboxIntervalSeconds }, w.publishBatch)
}

// publishBatch releases builds to Builder. Each owner's builds are released
// at most max_concurrent_builds at a time (see LeasePendingBuildOutbox), and a
// row whose build is no longer pending is discarded and never published.
func (w *BuildOutboxWorker) publishBatch(ctx context.Context) {
	cfg := w.cfg.Get()
	items, err := w.repo.LeasePendingBuildOutbox(ctx, cfg.BuildOutboxBatchSize, cfg.MaxConcurrentBuilds)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to lease build outbox messages", "error", err)
		return
	}

	for _, item := range items {
		build, err := w.repo.GetByID(ctx, item.BuildID)
		if err == nil && build.Status != model.BuildStatusPending {
			w.relay.logMarkFailure(ctx, "discarded", item.ID, w.repo.MarkBuildOutboxDiscarded(ctx, item.ID, nil), "build_id", item.BuildID)
			continue
		}
		if err != nil {
			if errors.Is(err, apperrors.ErrNotFound) {
				w.relay.logMarkFailure(ctx, "discarded", item.ID, w.repo.MarkBuildOutboxDiscarded(ctx, item.ID, err), "build_id", item.BuildID)
				continue
			}
			w.logger.ErrorContext(ctx, "failed to fetch build for outbox message", "build_id", item.BuildID, "outbox_id", item.ID, "error", err)
			w.relay.logMarkFailure(ctx, "pending", item.ID, w.repo.MarkBuildOutboxPending(ctx, item.ID, err), "build_id", item.BuildID)
			continue
		}
		w.relay.publish(ctx, outboxMessage{
			id:         item.ID,
			exchange:   item.Exchange,
			routingKey: item.RoutingKey,
			payload:    item.Payload,
			logAttrs:   []any{"build_id", item.BuildID},
		}, w.repo.MarkBuildOutboxPublished, w.repo.MarkBuildOutboxPending)
	}
}
