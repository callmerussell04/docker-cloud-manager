package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/callmerussell04/docker-cloud-manager/pkg/rabbitmqtopology"
	amqp "github.com/rabbitmq/amqp091-go"
)

type Consumer struct {
	url        string
	instanceID string
	logger     *slog.Logger
	wg         sync.WaitGroup
}

func NewConsumer(url, instanceID string, logger *slog.Logger) *Consumer {
	if instanceID == "" {
		instanceID = "default"
	}
	return &Consumer{
		url:        url,
		instanceID: instanceID,
		logger:     logging.WithComponent(logger, "build_queue_consumer").With("builder_instance_id", instanceID),
	}
}

func (c *Consumer) Run(ctx context.Context, workers int, handler func(context.Context, buildqueue.ImageBuildMessage) error) {
	if workers <= 0 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		workerID := i + 1
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.workerLoop(ctx, workerID, handler)
		}()
	}
}

func (c *Consumer) Stop(ctx context.Context) error {
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

func (c *Consumer) workerLoop(ctx context.Context, workerID int, handler func(context.Context, buildqueue.ImageBuildMessage) error) {
	logger := c.logger.With("worker_id", workerID)
	logger.InfoContext(ctx, "build queue worker started")
	defer logger.InfoContext(ctx, "build queue worker stopped")

	for {
		if ctx.Err() != nil {
			return
		}

		err := c.consume(ctx, workerID, handler)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.ErrorContext(ctx, "build queue consume loop failed", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (c *Consumer) consume(ctx context.Context, workerID int, handler func(context.Context, buildqueue.ImageBuildMessage) error) error {
	conn, err := amqp.Dial(c.url)
	if err != nil {
		return fmt.Errorf("failed to connect to rabbitmq: %w", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("failed to open rabbitmq channel: %w", err)
	}
	defer ch.Close()

	if err := rabbitmqtopology.DeclareBuild(ch); err != nil {
		return err
	}
	if err := ch.Qos(1, 0, false); err != nil {
		return fmt.Errorf("failed to set build queue prefetch: %w", err)
	}

	deliveries, err := ch.Consume(
		buildqueue.QueueName,
		fmt.Sprintf("builder-%s-%d", c.instanceID, workerID),
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to consume build queue: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("build queue delivery channel closed")
			}
			c.handleDelivery(ctx, delivery, handler)
		}
	}
}

func (c *Consumer) handleDelivery(ctx context.Context, delivery amqp.Delivery, handler func(context.Context, buildqueue.ImageBuildMessage) error) {
	var msg buildqueue.ImageBuildMessage
	if err := json.Unmarshal(delivery.Body, &msg); err != nil {
		c.logger.ErrorContext(ctx, "invalid build queue message", "error", err)
		_ = delivery.Nack(false, false)
		return
	}

	msgCtx := logging.ContextWithRequestID(ctx, msg.RequestID)
	if err := handler(msgCtx, msg); err != nil {
		c.logger.ErrorContext(msgCtx, "build queue message failed", "build_id", msg.BuildID, "error", err)
		_ = delivery.Nack(false, !isPermanentQueueError(err))
		return
	}
	_ = delivery.Ack(false)
}

func isPermanentQueueError(err error) bool {
	return errors.Is(err, apperrors.ErrBadRequest) ||
		errors.Is(err, apperrors.ErrForbidden) ||
		errors.Is(err, apperrors.ErrNotFound) ||
		errors.Is(err, apperrors.ErrAlreadyExists) ||
		errors.Is(err, apperrors.ErrConflict) ||
		errors.Is(err, apperrors.ErrLimitExceeded)
}
