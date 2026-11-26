package coretest

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/infrastructure/rabbitmq"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/buildqueue"
	platformrabbitmq "github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/rabbitmq"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/brokertest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/testenv"
)

// RabbitMQURLFromEnv returns the Core test broker URL, or ends t through its
// integration gate.
func RabbitMQURLFromEnv(t *testing.T) string {
	t.Helper()
	return testenv.Lookup(t, "CORE_TEST_RABBITMQ_URL")
}

// RabbitMQTopology returns a Core topology whose exchanges and queues are
// unique to t and deleted when t ends.
func RabbitMQTopology(t *testing.T, url string) rabbitmq.Topology {
	t.Helper()
	return rabbitmq.Topology{
		Build:     brokertest.UniqueQueue(t, url, buildqueue.Queue),
		Compose:   brokertest.UniqueQueue(t, url, queue.Compose),
		Container: brokertest.UniqueQueue(t, url, queue.Container),
		Resource:  brokertest.UniqueQueue(t, url, queue.Resource),
	}
}

// RoutedPublisher publishes to Topology what Publisher would send to a
// production exchange. Core services write production exchanges into outbox
// rows; routing keeps a test's relayed messages in that test's own queues.
type RoutedPublisher struct {
	Publisher service.OutboxPublisher
	Topology  rabbitmq.Topology
}

func (p RoutedPublisher) Publish(ctx context.Context, exchange, routingKey string, body []byte) error {
	for _, route := range []struct{ production, test platformrabbitmq.Queue }{
		{buildqueue.Queue, p.Topology.Build},
		{queue.Compose, p.Topology.Compose},
		{queue.Container, p.Topology.Container},
		{queue.Resource, p.Topology.Resource},
	} {
		if exchange == route.production.Exchange && route.test.Exchange != "" {
			exchange = route.test.Exchange
			break
		}
	}
	return p.Publisher.Publish(ctx, exchange, routingKey, body)
}

func (p RoutedPublisher) Close() error {
	return p.Publisher.Close()
}
