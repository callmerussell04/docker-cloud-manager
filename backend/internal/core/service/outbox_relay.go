package service

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/google/uuid"
)

// outboxRelay holds the scheduling and publication mechanics shared by the
// Core outbox workers. Each worker keeps its own lease query and the
// aggregate-specific policy that decides whether a leased record is published,
// returned to pending or discarded.
type outboxRelay struct {
	name      string
	publisher BuildQueuePublisher
	logger    *slog.Logger
}

func newOutboxRelay(name string, publisher BuildQueuePublisher, logger *slog.Logger) outboxRelay {
	return outboxRelay{
		name:      name,
		publisher: publisher,
		logger:    logging.WithComponent(logger, name+"_outbox_worker"),
	}
}

// schedule runs one batch immediately and then one batch per interval until ctx
// is canceled. The interval is read before each wait so runtime config changes
// apply; non-positive values fall back to one second. Cancellation is observed
// between batches; calls within a batch receive the canceled context.
func (r outboxRelay) schedule(ctx context.Context, intervalSeconds func() int64, publishBatch func(context.Context)) {
	r.logger.InfoContext(ctx, r.name+" outbox worker started")
	publishBatch(ctx)
	for {
		interval := time.Duration(intervalSeconds()) * time.Second
		if interval <= 0 {
			interval = time.Second
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			r.logger.InfoContext(ctx, r.name+" outbox worker stopped")
			return
		case <-timer.C:
			publishBatch(ctx)
		}
	}
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
// publication returns it to pending with the cause. A failed published mark is
// logged and the record is redelivered after its lease expires.
func (r outboxRelay) publish(
	ctx context.Context,
	msg outboxMessage,
	markPublished func(context.Context, uuid.UUID) error,
	markPending func(context.Context, uuid.UUID, error) error,
) {
	if err := r.publisher.Publish(ctx, msg.exchange, msg.routingKey, msg.payload); err != nil {
		r.logger.ErrorContext(ctx, "failed to publish "+r.name+" queue message", msg.failureAttrs(err)...)
		_ = markPending(ctx, msg.id, err)
		return
	}
	if err := markPublished(ctx, msg.id); err != nil {
		r.logger.ErrorContext(ctx, "failed to mark "+r.name+" outbox message published", msg.failureAttrs(err)...)
	}
}

func (m outboxMessage) failureAttrs(err error) []any {
	return append(slices.Clip(m.logAttrs), "outbox_id", m.id, "error", err)
}
