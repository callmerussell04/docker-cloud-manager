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
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/callmerussell04/docker-cloud-manager/pkg/resourcequeue"
	amqp "github.com/rabbitmq/amqp091-go"
)

type ResourceConsumer struct {
	url        string
	instanceID string
	logger     *slog.Logger
	wg         sync.WaitGroup
}

func NewResourceConsumer(url, instanceID string, logger *slog.Logger) *ResourceConsumer {
	if instanceID == "" {
		instanceID = "core"
	}
	return &ResourceConsumer{
		url:        url,
		instanceID: instanceID,
		logger:     logging.WithComponent(logger, "resource_queue_consumer").With("core_instance_id", instanceID),
	}
}

func (c *ResourceConsumer) Run(ctx context.Context, workers int, handler func(context.Context, resourcequeue.LifecycleMessage) error) {
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

func (c *ResourceConsumer) Stop(ctx context.Context) error {
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

func (c *ResourceConsumer) workerLoop(ctx context.Context, workerID int, handler func(context.Context, resourcequeue.LifecycleMessage) error) {
	logger := c.logger.With("worker_id", workerID)
	logger.InfoContext(ctx, "resource queue worker started")
	defer logger.InfoContext(ctx, "resource queue worker stopped")
	for {
		if ctx.Err() != nil {
			return
		}
		err := c.consume(ctx, workerID, handler)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.ErrorContext(ctx, "resource queue consume loop failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (c *ResourceConsumer) consume(ctx context.Context, workerID int, handler func(context.Context, resourcequeue.LifecycleMessage) error) error {
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
	if err := declareTopology(ch); err != nil {
		return err
	}
	if err := ch.Qos(1, 0, false); err != nil {
		return fmt.Errorf("failed to set resource queue prefetch: %w", err)
	}
	deliveries, err := ch.Consume(resourcequeue.QueueName, fmt.Sprintf("core-%s-resource-%d", c.instanceID, workerID), false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("failed to consume resource queue: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("resource queue delivery channel closed")
			}
			c.handleDelivery(ctx, delivery, handler)
		}
	}
}

func (c *ResourceConsumer) handleDelivery(ctx context.Context, delivery amqp.Delivery, handler func(context.Context, resourcequeue.LifecycleMessage) error) {
	var msg resourcequeue.LifecycleMessage
	if err := json.Unmarshal(delivery.Body, &msg); err != nil {
		c.logger.ErrorContext(ctx, "invalid resource queue message", "error", err)
		_ = delivery.Nack(false, false)
		return
	}
	msgCtx := logging.ContextWithRequestID(ctx, msg.RequestID)
	if err := handler(msgCtx, msg); err != nil {
		c.logger.ErrorContext(msgCtx, "resource queue message failed", "resource_type", msg.ResourceType, "resource_id", msg.ResourceID, "operation_id", msg.OperationID, "error", err)
		_ = delivery.Nack(false, !isPermanentResourceQueueError(err))
		return
	}
	_ = delivery.Ack(false)
}

func isPermanentResourceQueueError(err error) bool {
	return errors.Is(err, apperrors.ErrBadRequest) ||
		errors.Is(err, apperrors.ErrForbidden) ||
		errors.Is(err, apperrors.ErrNotFound) ||
		errors.Is(err, apperrors.ErrAlreadyExists) ||
		errors.Is(err, apperrors.ErrConflict) ||
		errors.Is(err, apperrors.ErrLimitExceeded)
}
