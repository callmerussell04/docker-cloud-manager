package rabbitmq

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/pkg/buildqueue"
	amqp "github.com/rabbitmq/amqp091-go"
)

type Publisher struct {
	url      string
	mu       sync.Mutex
	conn     *amqp.Connection
	ch       *amqp.Channel
	confirms <-chan amqp.Confirmation
	closed   bool
}

func NewPublisher(url string) *Publisher {
	return &Publisher{url: url}
}

func (p *Publisher) Publish(ctx context.Context, exchange, routingKey string, body []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return fmt.Errorf("rabbitmq publisher is closed")
	}
	if err := p.ensureConnected(); err != nil {
		return err
	}

	if err := p.ch.PublishWithContext(ctx, exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
		Body:         body,
	}); err != nil {
		p.resetLocked()
		return fmt.Errorf("failed to publish build queue message: %w", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case confirm, ok := <-p.confirms:
		if !ok {
			p.resetLocked()
			return fmt.Errorf("rabbitmq publisher confirms channel closed")
		}
		if !confirm.Ack {
			return fmt.Errorf("rabbitmq rejected build queue message")
		}
		return nil
	}
}

func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return p.resetLocked()
}

func (p *Publisher) ensureConnected() error {
	if p.conn != nil && !p.conn.IsClosed() && p.ch != nil {
		return nil
	}
	p.resetLocked()

	conn, err := amqp.Dial(p.url)
	if err != nil {
		return fmt.Errorf("failed to connect to rabbitmq: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to open rabbitmq channel: %w", err)
	}
	if err := declareBuildTopology(ch); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to enable publisher confirms: %w", err)
	}
	p.conn = conn
	p.ch = ch
	p.confirms = ch.NotifyPublish(make(chan amqp.Confirmation, 1))
	return nil
}

func (p *Publisher) resetLocked() error {
	var err error
	if p.ch != nil {
		if closeErr := p.ch.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	if p.conn != nil {
		if closeErr := p.conn.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	p.ch = nil
	p.conn = nil
	p.confirms = nil
	return err
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
