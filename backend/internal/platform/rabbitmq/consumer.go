package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	amqp "github.com/rabbitmq/amqp091-go"
)

const reconnectDelay = 5 * time.Second

// ConsumerConfig is what a workload supplies to the shared consume loop. Every
// field is required.
type ConsumerConfig[M any] struct {
	URL   string
	Queue string
	// ConsumerTag names a worker's broker consumer; workerID starts at 1.
	ConsumerTag func(workerID int) string
	// Declare runs on every connection, before consuming.
	Declare func(ch *amqp.Channel) error
	// IsPermanent reports whether a handler error dead-letters the message
	// instead of requeueing it.
	IsPermanent func(err error) bool
	RequestID   func(msg M) string
	// LogFields returns the slog key-value pairs identifying a failed message.
	LogFields func(msg M) []any
	Logger    *slog.Logger
	// RetryDelay is how long a worker holds a delivery whose handler failed
	// with a retryable error before returning it to the queue. A requeue does
	// not count against a quorum queue's delivery limit, so without the wait a
	// failing message would be redelivered in a tight loop. It must be
	// positive.
	RetryDelay time.Duration
}

// Consumer runs manually acknowledged workers with prefetch 1. Each worker
// reconnects after any connection or channel failure until its context ends.
type Consumer[M any] struct {
	cfg    ConsumerConfig[M]
	logger *slog.Logger
	wg     sync.WaitGroup
}

// NewConsumer refuses a configuration without a positive RetryDelay, which
// would bring back the tight redelivery loop.
func NewConsumer[M any](cfg ConsumerConfig[M]) (*Consumer[M], error) {
	if cfg.RetryDelay <= 0 {
		return nil, fmt.Errorf("%s queue consumer: retry delay must be positive", cfg.Queue)
	}
	return &Consumer[M]{cfg: cfg, logger: cfg.Logger.With("queue", cfg.Queue)}, nil
}

// Run starts the workers, at least one, and returns immediately. They stop when
// ctx ends; Stop waits for them.
func (c *Consumer[M]) Run(ctx context.Context, workers int, handler func(context.Context, M) error) {
	if workers <= 0 {
		workers = 1
	}
	for i := range workers {
		c.wg.Go(func() {
			c.workerLoop(ctx, i+1, handler)
		})
	}
}

// Stop waits for the workers until ctx ends. It does not cancel them.
func (c *Consumer[M]) Stop(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (c *Consumer[M]) workerLoop(ctx context.Context, workerID int, handler func(context.Context, M) error) {
	logger := c.logger.With("worker_id", workerID)
	logger.InfoContext(ctx, "queue worker started")
	defer logger.InfoContext(ctx, "queue worker stopped")

	for {
		if ctx.Err() != nil {
			return
		}
		err := c.consume(ctx, workerID, handler)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.ErrorContext(ctx, "queue consume loop failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectDelay):
		}
	}
}

func (c *Consumer[M]) consume(ctx context.Context, workerID int, handler func(context.Context, M) error) error {
	conn, err := amqp.Dial(c.cfg.URL)
	if err != nil {
		return fmt.Errorf("failed to connect to rabbitmq: %w", err)
	}
	defer func() { _ = conn.Close() }() // connection teardown; close failure is not actionable

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("failed to open rabbitmq channel: %w", err)
	}
	defer func() { _ = ch.Close() }() // connection teardown; close failure is not actionable

	if err := c.cfg.Declare(ch); err != nil {
		return err
	}
	if err := ch.Qos(1, 0, false); err != nil {
		return fmt.Errorf("failed to set queue prefetch: %w", err)
	}
	deliveries, err := ch.Consume(c.cfg.Queue, c.cfg.ConsumerTag(workerID), false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("failed to consume queue: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return errors.New("queue delivery channel closed")
			}
			c.handleDelivery(ctx, delivery, handler)
		}
	}
}

func (c *Consumer[M]) handleDelivery(ctx context.Context, delivery amqp.Delivery, handler func(context.Context, M) error) {
	var msg M
	if err := json.Unmarshal(delivery.Body, &msg); err != nil {
		c.logger.ErrorContext(ctx, "invalid queue message", "error", err)
		c.nack(ctx, delivery, false)
		return
	}
	msgCtx := logging.ContextWithRequestID(ctx, c.cfg.RequestID(msg))
	if err := handler(msgCtx, msg); err != nil {
		c.logger.With(c.cfg.LogFields(msg)...).ErrorContext(msgCtx, "queue message failed", "error", err)
		requeue := !c.cfg.IsPermanent(err)
		if requeue {
			c.waitBeforeRetry(ctx)
		}
		c.nack(msgCtx, delivery, requeue)
		return
	}
	// A failed ack means the channel closed, so the broker redelivers the
	// message to the idempotent handler.
	if err := delivery.Ack(false); err != nil {
		c.logger.WarnContext(msgCtx, "failed to acknowledge queue message", "error", err)
	}
}

// waitBeforeRetry holds the delivery for RetryDelay, or until ctx ends so
// that shutdown returns the delivery at once.
func (c *Consumer[M]) waitBeforeRetry(ctx context.Context) {
	timer := time.NewTimer(c.cfg.RetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// nack rejects a delivery. A failed nack means the channel closed, so the
// broker redelivers the message.
func (c *Consumer[M]) nack(ctx context.Context, delivery amqp.Delivery, requeue bool) {
	if err := delivery.Nack(false, requeue); err != nil {
		c.logger.WarnContext(ctx, "failed to reject queue message", "requeue", requeue, "error", err)
	}
}

// IsPermanentAppError reports whether err is an application error that a retry
// cannot fix. A workload with retryable application errors wraps this rule.
func IsPermanentAppError(err error) bool {
	return errors.Is(err, apperrors.ErrBadRequest) ||
		errors.Is(err, apperrors.ErrForbidden) ||
		errors.Is(err, apperrors.ErrNotFound) ||
		errors.Is(err, apperrors.ErrAlreadyExists) ||
		errors.Is(err, apperrors.ErrConflict) ||
		errors.Is(err, apperrors.ErrLimitExceeded)
}
