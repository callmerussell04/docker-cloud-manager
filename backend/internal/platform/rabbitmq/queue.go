// Package rabbitmq holds queue declaration from a description and the one
// consume loop shared by every queue workload.
package rabbitmq

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Queue describes a durable quorum queue, its direct exchange and its
// dead-letter route.
type Queue struct {
	Label      string // names the queue in declaration errors
	Exchange   string
	Name       string
	RoutingKey string
	DLX        string
	DLQ        string
	DLQKey     string
}

// Declare idempotently declares each queue in order, stopping at the first failure.
func Declare(ch *amqp.Channel, queues ...Queue) error {
	for _, queue := range queues {
		if err := queue.declare(ch); err != nil {
			return err
		}
	}
	return nil
}

func (q Queue) declare(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(q.Exchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("failed to declare %s exchange: %w", q.Label, err)
	}
	if err := ch.ExchangeDeclare(q.DLX, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("failed to declare %s dlx: %w", q.Label, err)
	}
	if _, err := ch.QueueDeclare(q.DLQ, true, false, false, false, amqp.Table{
		"x-queue-type": "quorum",
	}); err != nil {
		return fmt.Errorf("failed to declare %s dlq: %w", q.Label, err)
	}
	if err := ch.QueueBind(q.DLQ, q.DLQKey, q.DLX, false, nil); err != nil {
		return fmt.Errorf("failed to bind %s dlq: %w", q.Label, err)
	}
	if _, err := ch.QueueDeclare(q.Name, true, false, false, false, amqp.Table{
		"x-queue-type":              "quorum",
		"x-dead-letter-exchange":    q.DLX,
		"x-dead-letter-routing-key": q.DLQKey,
	}); err != nil {
		return fmt.Errorf("failed to declare %s queue: %w", q.Label, err)
	}
	if err := ch.QueueBind(q.Name, q.RoutingKey, q.Exchange, false, nil); err != nil {
		return fmt.Errorf("failed to bind %s queue: %w", q.Label, err)
	}
	return nil
}
