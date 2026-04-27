package rabbitmq

import (
	"context"
	"fmt"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/pkg/buildqueue"
	amqp "github.com/rabbitmq/amqp091-go"
)

type Publisher struct {
	url string
}

func NewPublisher(url string) *Publisher {
	return &Publisher{url: url}
}

func (p *Publisher) Publish(ctx context.Context, exchange, routingKey string, body []byte) error {
	conn, err := amqp.Dial(p.url)
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
	if err := ch.Confirm(false); err != nil {
		return fmt.Errorf("failed to enable publisher confirms: %w", err)
	}
	confirms := ch.NotifyPublish(make(chan amqp.Confirmation, 1))

	if err := ch.PublishWithContext(ctx, exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
		Body:         body,
	}); err != nil {
		return fmt.Errorf("failed to publish build queue message: %w", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case confirm := <-confirms:
		if !confirm.Ack {
			return fmt.Errorf("rabbitmq rejected build queue message")
		}
		return nil
	}
}

func (p *Publisher) Close() error {
	return nil
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
