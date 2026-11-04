package rabbitmq

import (
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/buildqueue"
	platformrabbitmq "github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/rabbitmq"
	amqp "github.com/rabbitmq/amqp091-go"
)

// Topology names the queues Core declares and consumes. A zero queue selects
// its production description, so the zero Topology is production's.
type Topology struct {
	Build     platformrabbitmq.Queue
	Compose   platformrabbitmq.Queue
	Container platformrabbitmq.Queue
	Resource  platformrabbitmq.Queue
}

func (t Topology) withDefaults() Topology {
	t.Build = orDefault(t.Build, buildqueue.Queue)
	t.Compose = orDefault(t.Compose, queue.Compose)
	t.Container = orDefault(t.Container, queue.Container)
	t.Resource = orDefault(t.Resource, queue.Resource)
	return t
}

func (t Topology) declare(ch *amqp.Channel) error {
	return platformrabbitmq.Declare(ch, t.Build, t.Compose, t.Container, t.Resource)
}

func orDefault(q, fallback platformrabbitmq.Queue) platformrabbitmq.Queue {
	if q == (platformrabbitmq.Queue{}) {
		return fallback
	}
	return q
}
