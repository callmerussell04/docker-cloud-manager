package rabbitmqtopology

import (
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/pkg/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/composequeue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/containerqueue"
	amqp "github.com/rabbitmq/amqp091-go"
)

func DeclareBuild(ch *amqp.Channel) error {
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

func DeclareCompose(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(composequeue.ExchangeName, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("failed to declare compose exchange: %w", err)
	}
	if err := ch.ExchangeDeclare(composequeue.DLXName, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("failed to declare compose dlx: %w", err)
	}
	if _, err := ch.QueueDeclare(composequeue.DLQName, true, false, false, false, amqp.Table{
		"x-queue-type": "quorum",
	}); err != nil {
		return fmt.Errorf("failed to declare compose dlq: %w", err)
	}
	if err := ch.QueueBind(composequeue.DLQName, composequeue.DLQKey, composequeue.DLXName, false, nil); err != nil {
		return fmt.Errorf("failed to bind compose dlq: %w", err)
	}
	if _, err := ch.QueueDeclare(composequeue.QueueName, true, false, false, false, amqp.Table{
		"x-queue-type":              "quorum",
		"x-dead-letter-exchange":    composequeue.DLXName,
		"x-dead-letter-routing-key": composequeue.DLQKey,
	}); err != nil {
		return fmt.Errorf("failed to declare compose queue: %w", err)
	}
	if err := ch.QueueBind(composequeue.QueueName, composequeue.RoutingKey, composequeue.ExchangeName, false, nil); err != nil {
		return fmt.Errorf("failed to bind compose queue: %w", err)
	}
	return nil
}

func DeclareContainer(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(containerqueue.ExchangeName, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("failed to declare container exchange: %w", err)
	}
	if err := ch.ExchangeDeclare(containerqueue.DLXName, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("failed to declare container dlx: %w", err)
	}
	if _, err := ch.QueueDeclare(containerqueue.DLQName, true, false, false, false, amqp.Table{
		"x-queue-type": "quorum",
	}); err != nil {
		return fmt.Errorf("failed to declare container dlq: %w", err)
	}
	if err := ch.QueueBind(containerqueue.DLQName, containerqueue.DLQKey, containerqueue.DLXName, false, nil); err != nil {
		return fmt.Errorf("failed to bind container dlq: %w", err)
	}
	if _, err := ch.QueueDeclare(containerqueue.QueueName, true, false, false, false, amqp.Table{
		"x-queue-type":              "quorum",
		"x-dead-letter-exchange":    containerqueue.DLXName,
		"x-dead-letter-routing-key": containerqueue.DLQKey,
	}); err != nil {
		return fmt.Errorf("failed to declare container queue: %w", err)
	}
	if err := ch.QueueBind(containerqueue.QueueName, containerqueue.RoutingKey, containerqueue.ExchangeName, false, nil); err != nil {
		return fmt.Errorf("failed to bind container queue: %w", err)
	}
	return nil
}

func DeclareAll(ch *amqp.Channel) error {
	if err := DeclareBuild(ch); err != nil {
		return err
	}
	if err := DeclareCompose(ch); err != nil {
		return err
	}
	return DeclareContainer(ch)
}
