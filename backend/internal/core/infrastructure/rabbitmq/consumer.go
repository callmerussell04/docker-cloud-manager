package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/pkg/composequeue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	amqp "github.com/rabbitmq/amqp091-go"
)

type ComposeConsumer struct {
	url        string
	instanceID string
	logger     *slog.Logger
}

func NewComposeConsumer(url, instanceID string, logger *slog.Logger) *ComposeConsumer {
	if instanceID == "" {
		instanceID = "core"
	}
	return &ComposeConsumer{
		url:        url,
		instanceID: instanceID,
		logger:     logging.WithComponent(logger, "compose_queue_consumer").With("core_instance_id", instanceID),
	}
}

func (c *ComposeConsumer) Run(ctx context.Context, workers int, handler func(context.Context, composequeue.DeploymentMessage) error) {
	if workers <= 0 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		workerID := i + 1
		go c.workerLoop(ctx, workerID, handler)
	}
}

func (c *ComposeConsumer) workerLoop(ctx context.Context, workerID int, handler func(context.Context, composequeue.DeploymentMessage) error) {
	logger := c.logger.With("worker_id", workerID)
	logger.InfoContext(ctx, "compose queue worker started")
	defer logger.InfoContext(ctx, "compose queue worker stopped")

	for {
		if ctx.Err() != nil {
			return
		}
		err := c.consume(ctx, workerID, handler)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.ErrorContext(ctx, "compose queue consume loop failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (c *ComposeConsumer) consume(ctx context.Context, workerID int, handler func(context.Context, composequeue.DeploymentMessage) error) error {
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
		return fmt.Errorf("failed to set compose queue prefetch: %w", err)
	}
	deliveries, err := ch.Consume(
		composequeue.QueueName,
		fmt.Sprintf("core-%s-%d", c.instanceID, workerID),
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to consume compose queue: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("compose queue delivery channel closed")
			}
			c.handleDelivery(ctx, delivery, handler)
		}
	}
}

func (c *ComposeConsumer) handleDelivery(ctx context.Context, delivery amqp.Delivery, handler func(context.Context, composequeue.DeploymentMessage) error) {
	var msg composequeue.DeploymentMessage
	if err := json.Unmarshal(delivery.Body, &msg); err != nil {
		c.logger.ErrorContext(ctx, "invalid compose queue message", "error", err)
		_ = delivery.Nack(false, false)
		return
	}
	msgCtx := logging.ContextWithRequestID(ctx, msg.RequestID)
	if err := handler(msgCtx, msg); err != nil {
		c.logger.ErrorContext(msgCtx, "compose queue message failed", "job_id", msg.JobID, "project_id", msg.ProjectID, "error", err)
		_ = delivery.Nack(false, true)
		return
	}
	_ = delivery.Ack(false)
}
