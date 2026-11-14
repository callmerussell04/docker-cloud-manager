package rabbitmq_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/infrastructure/rabbitmq"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/buildqueue"
	platformrabbitmq "github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/rabbitmq"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
)

func TestRabbitMQPublisherDeclaresTopologyAndPublishes(t *testing.T) {
	url := coretest.RabbitMQURLFromEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	topology := coretest.RabbitMQTopology(t, url)
	ch := openChannel(t, url)

	publisher := rabbitmq.NewPublisher(url, topology)
	t.Cleanup(func() { _ = publisher.Close() })
	payload := []byte(`{"build_id":"` + uuid.NewString() + `"}`)
	require.NoError(t, publisher.Publish(ctx, topology.Build.Exchange, topology.Build.RoutingKey, payload))

	msg, ok, err := ch.Get(topology.Build.Name, true)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "application/json", msg.ContentType)
	require.Equal(t, uint8(amqp.Persistent), msg.DeliveryMode)
	require.JSONEq(t, string(payload), string(msg.Body))
}

func TestRabbitMQContainerConsumerPermanentNack(t *testing.T) {
	url := coretest.RabbitMQURLFromEnv(t)
	topology := coretest.RabbitMQTopology(t, url)
	declareTopology(t, url, topology)
	ch := openChannel(t, url)

	ctx, cancel := context.WithCancel(context.Background())
	consumer := platformrabbitmq.NewConsumer(rabbitmq.ContainerConsumerConfig(url, "test-"+uuid.NewString()[:8], topology, coretest.DiscardLogger()))
	handled := make(chan queue.ContainerLifecycleMessage, 1)
	consumer.Run(ctx, 1, func(ctx context.Context, msg queue.ContainerLifecycleMessage) error {
		handled <- msg
		return apperrors.ErrConflict
	})
	t.Cleanup(func() {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = consumer.Stop(stopCtx)
		stopCancel()
	})

	publishRaw(t, ch, topology.Container.Exchange, topology.Container.RoutingKey, mustJSON(t, queue.ContainerLifecycleMessage{
		OperationID: uuid.NewString(),
		ContainerID: uuid.NewString(),
		OwnerID:     uuid.NewString(),
		Operation:   model.OperationStart,
	}))
	require.Eventually(t, func() bool {
		return len(handled) == 1
	}, 5*time.Second, 50*time.Millisecond)
	requireQueueEmpty(t, ch, topology.Container.Name)
}

func TestRabbitMQBuildOutboxWorkerPublishesAndMarksPublished(t *testing.T) {
	url := coretest.RabbitMQURLFromEnv(t)
	ctx := context.Background()
	repos := coretest.OpenCoreRepositories(t)
	topology := coretest.RabbitMQTopology(t, url)
	declareTopology(t, url, topology)
	ch := openChannel(t, url)

	ownerID := uuid.New()
	imageID := uuid.New()
	buildID := uuid.New()
	outboxID := uuid.New()
	payload := mustJSON(t, buildqueue.ImageBuildMessage{BuildID: buildID.String(), ImageID: imageID.String(), OwnerID: ownerID.String(), Tag: "demo:latest"})
	require.NoError(t, repos.Builds.CreateQueuedBuild(ctx,
		model.Image{ID: imageID, RegistryTag: imageID.String(), OwnerID: ownerID, Tag: "demo:latest", Status: model.ImageStatusBuilding},
		model.Build{ID: buildID, ImageID: imageID, OwnerID: ownerID, Status: model.BuildStatusPending, LogObjectKey: "logs", ArchiveObjectKey: "archive"},
		model.BuildQueueOutbox{ID: outboxID, BuildID: buildID, Exchange: topology.Build.Exchange, RoutingKey: topology.Build.RoutingKey, Payload: payload},
	))

	publisher := rabbitmq.NewPublisher(url, topology)
	workerCtx, cancel := context.WithCancel(context.Background())
	worker := service.NewBuildOutboxWorker(repos.Builds, publisher, coretest.StaticConfig{Config: coretest.IntegrationConfig()}, coretest.DiscardLogger())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(workerCtx)
	}()
	require.Eventually(t, func() bool {
		var status string
		err := repos.DB.QueryRow(context.Background(), `SELECT status FROM build_queue_outbox WHERE id = $1`, outboxID).Scan(&status)
		return err == nil && status == model.BuildOutboxStatusPublished
	}, 5*time.Second, 50*time.Millisecond)
	cancel()
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 5*time.Second, 50*time.Millisecond)
	require.NoError(t, publisher.Close())

	msg, ok, err := ch.Get(topology.Build.Name, true)
	require.NoError(t, err)
	require.True(t, ok)
	require.JSONEq(t, string(payload), string(msg.Body))
}

func openChannel(t *testing.T, url string) *amqp.Channel {
	t.Helper()
	conn, err := amqp.Dial(url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	ch, err := conn.Channel()
	require.NoError(t, err)
	t.Cleanup(func() { _ = ch.Close() })
	return ch
}

// declareTopology declares topology before a test publishes to it directly.
func declareTopology(t *testing.T, url string, topology rabbitmq.Topology) {
	t.Helper()
	require.NoError(t, platformrabbitmq.Declare(openChannel(t, url), topology.Build, topology.Compose, topology.Container, topology.Resource))
}

func publishRaw(t *testing.T, ch *amqp.Channel, exchange, routingKey string, body []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, ch.PublishWithContext(ctx, exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	}))
}

func requireQueueEmpty(t *testing.T, ch *amqp.Channel, name string) {
	t.Helper()
	require.Eventually(t, func() bool {
		msg, ok, err := ch.Get(name, false)
		if err != nil {
			return false
		}
		if ok {
			_ = msg.Nack(false, true)
			return false
		}
		return true
	}, 2*time.Second, 50*time.Millisecond)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	return data
}
