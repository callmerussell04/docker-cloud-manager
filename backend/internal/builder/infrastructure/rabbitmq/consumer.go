package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/pkg/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	amqp "github.com/rabbitmq/amqp091-go"
)

type Consumer struct {
	url        string
	instanceID string
	logger     *slog.Logger
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
		go c.workerLoop(ctx, workerID, handler)
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

	if err := declareBuildTopology(ch); err != nil {
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
		_ = delivery.Nack(false, true)
		return
	}
	_ = delivery.Ack(false)
}

func declareBuildTopology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(buildqueue.ExchangeName, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("failed to declare build exchange: %w", err)
	}
	if err := ch.ExchangeDeclare(buildqueue.DLXName, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("failed to declare build dlx: %w", err)
	}
	if _, err := ch.QueueDeclare(buildqueue.DLQName, true, false, false, false, amqp.Table{
		"x-queue-type": "quorum",
	}); err != nil {
		return fmt.Errorf("failed to declare build dlq: %w", err)
	}
	if err := ch.QueueBind(buildqueue.DLQName, buildqueue.DLQKey, buildqueue.DLXName, false, nil); err != nil {
		return fmt.Errorf("failed to bind build dlq: %w", err)
	}
	if _, err := ch.QueueDeclare(buildqueue.QueueName, true, false, false, false, amqp.Table{
		"x-queue-type":              "quorum",
		"x-dead-letter-exchange":    buildqueue.DLXName,
		"x-dead-letter-routing-key": buildqueue.DLQKey,
	}); err != nil {
		return fmt.Errorf("failed to declare build queue: %w", err)
	}
	if err := ch.QueueBind(buildqueue.QueueName, buildqueue.RoutingKey, buildqueue.ExchangeName, false, nil); err != nil {
		return fmt.Errorf("failed to bind build queue: %w", err)
	}
	return nil
}
