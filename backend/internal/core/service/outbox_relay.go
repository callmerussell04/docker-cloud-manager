package service

import (
	"context"
	"log/slog"
	"slices"

	"github.com/google/uuid"
)

// OutboxPublisher delivers leased build, Compose, container and resource
// outbox records to their RabbitMQ queues with broker confirmation.
type OutboxPublisher interface {
	Publish(ctx context.Context, exchange, routingKey string, body []byte) error
	Close() error
}

// outboxRelay holds the scheduling and publication mechanics shared by the
// Core outbox workers. Each worker keeps its own lease query and the
// aggregate-specific policy that decides whether a leased record is published,
// returned to pending or discarded.
type outboxRelay struct {
	name      string
	publisher OutboxPublisher
	logger    *slog.Logger
}

// newOutboxRelay names the queue class in log messages; logger is the worker's
// component logger.
func newOutboxRelay(name string, publisher OutboxPublisher, logger *slog.Logger) outboxRelay {
	return outboxRelay{name: name, publisher: publisher, logger: logger}
}

// run runs one batch immediately and then one batch per interval until ctx
// is canceled, on the shared periodic schedule.
func (r outboxRelay) run(ctx context.Context, intervalSeconds func() int64, publishBatch func(context.Context)) {
	r.logger.InfoContext(ctx, r.name+" outbox worker started")
	publishBatch(ctx)
	runPeriodically(ctx, intervalSeconds, publishBatch)
	r.logger.InfoContext(ctx, r.name+" outbox worker stopped")
}

// outboxMessage is a leased record whose aggregate policy allows publication.
type outboxMessage struct {
	id         uuid.UUID
	exchange   string
	routingKey string
	payload    []byte
	// logAttrs identify the record's aggregate in failure logs.
	logAttrs []any
}

// publish marks the record published only after broker confirmation. A failed
// publication returns it to pending with the cause.
func (r outboxRelay) publish(
	ctx context.Context,
	msg outboxMessage,
	markPublished func(context.Context, uuid.UUID) error,
	markPending func(context.Context, uuid.UUID, error) error,
) {
	if err := r.publisher.Publish(ctx, msg.exchange, msg.routingKey, msg.payload); err != nil {
		r.logger.ErrorContext(ctx, "failed to publish "+r.name+" queue message", failureAttrs(msg.id, err, msg.logAttrs...)...)
		r.logMarkFailure(ctx, "pending", msg.id, markPending(ctx, msg.id, err), msg.logAttrs...)
		return
	}
	r.logMarkFailure(ctx, "published", msg.id, markPublished(ctx, msg.id), msg.logAttrs...)
}

// logMarkFailure logs a failed status transition of a leased record once. The record
// keeps its lease and is redelivered after the lease expires, so the worker
// continues the batch.
func (r outboxRelay) logMarkFailure(ctx context.Context, transition string, id uuid.UUID, err error, logAttrs ...any) {
	if err != nil {
		r.logger.ErrorContext(ctx, "failed to mark "+r.name+" outbox message "+transition, failureAttrs(id, err, logAttrs...)...)
	}
}

func failureAttrs(id uuid.UUID, err error, logAttrs ...any) []any {
	return append(slices.Clip(logAttrs), "outbox_id", id, "error", err)
}
