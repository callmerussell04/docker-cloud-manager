package rabbitmq_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/rabbitmq"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/composequeue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/containerqueue"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/coretest"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
)

func TestRabbitMQPublisherDeclaresTopologyAndPublishes(t *testing.T) {
	url := coretest.RabbitMQURLFromEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	declareTopology(t, url)
	ch := openChannel(t, url)
	purgeQueues(t, ch)

	publisher := rabbitmq.NewPublisher(url)
	t.Cleanup(func() { _ = publisher.Close() })
	payload := []byte(`{"build_id":"` + uuid.NewString() + `"}`)
	require.NoError(t, publisher.Publish(ctx, buildqueue.ExchangeName, buildqueue.RoutingKey, payload))

	msg, ok, err := ch.Get(buildqueue.QueueName, true)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "application/json", msg.ContentType)
	require.Equal(t, uint8(amqp.Persistent), msg.DeliveryMode)
	require.JSONEq(t, string(payload), string(msg.Body))
}

func TestRabbitMQComposeConsumerAckAndPermanentNack(t *testing.T) {
	url := coretest.RabbitMQURLFromEnv(t)
	declareTopology(t, url)
	ch := openChannel(t, url)
	purgeQueues(t, ch)

	ctx, cancel := context.WithCancel(context.Background())
	consumer := rabbitmq.NewComposeConsumer(url, "test-"+uuid.NewString()[:8], coretest.DiscardLogger())
	handled := make(chan composequeue.DeploymentMessage, 2)
	consumer.Run(ctx, 1, func(ctx context.Context, msg composequeue.DeploymentMessage) error {
		handled <- msg
		if msg.ProjectID == "permanent" {
			return apperrors.ErrBadRequest
		}
		return nil
	})
	t.Cleanup(func() {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = consumer.Stop(stopCtx)
		stopCancel()
	})

	publishRaw(t, ch, composequeue.ExchangeName, composequeue.RoutingKey, mustJSON(t, composequeue.DeploymentMessage{JobID: uuid.NewString(), ProjectID: uuid.NewString()}))
	require.Eventually(t, func() bool {
		return len(handled) == 1
	}, 5*time.Second, 50*time.Millisecond)
	requireQueueEmpty(t, ch, composequeue.QueueName)

	publishRaw(t, ch, composequeue.ExchangeName, composequeue.RoutingKey, mustJSON(t, composequeue.DeploymentMessage{JobID: uuid.NewString(), ProjectID: "permanent"}))
	require.Eventually(t, func() bool {
		return len(handled) == 2
	}, 5*time.Second, 50*time.Millisecond)
	requireQueueEmpty(t, ch, composequeue.QueueName)

	publishRaw(t, ch, composequeue.ExchangeName, composequeue.RoutingKey, []byte(`not-json`))
	require.Never(t, func() bool {
		return len(handled) > 2
	}, 300*time.Millisecond, 50*time.Millisecond)
	requireQueueEmpty(t, ch, composequeue.QueueName)
}

func TestRabbitMQContainerConsumerPermanentNack(t *testing.T) {
	url := coretest.RabbitMQURLFromEnv(t)
	declareTopology(t, url)
	ch := openChannel(t, url)
	purgeQueues(t, ch)

	ctx, cancel := context.WithCancel(context.Background())
	consumer := rabbitmq.NewContainerConsumer(url, "test-"+uuid.NewString()[:8], coretest.DiscardLogger())
	handled := make(chan containerqueue.LifecycleMessage, 1)
	consumer.Run(ctx, 1, func(ctx context.Context, msg containerqueue.LifecycleMessage) error {
		handled <- msg
		return apperrors.ErrConflict
	})
	t.Cleanup(func() {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = consumer.Stop(stopCtx)
		stopCancel()
	})

	publishRaw(t, ch, containerqueue.ExchangeName, containerqueue.RoutingKey, mustJSON(t, containerqueue.LifecycleMessage{
		OperationID: uuid.NewString(),
		ContainerID: uuid.NewString(),
		OwnerID:     uuid.NewString(),
		Operation:   model.OperationStart,
	}))
	require.Eventually(t, func() bool {
		return len(handled) == 1
	}, 5*time.Second, 50*time.Millisecond)
	requireQueueEmpty(t, ch, containerqueue.QueueName)
}

func TestRabbitMQBuildOutboxWorkerPublishesAndMarksPublished(t *testing.T) {
	url := coretest.RabbitMQURLFromEnv(t)
	ctx := context.Background()
	repos := coretest.OpenCoreRepositories(t)
	declareTopology(t, url)
	ch := openChannel(t, url)
	purgeQueues(t, ch)

	ownerID := uuid.New()
	imageID := uuid.New()
	buildID := uuid.New()
	outboxID := uuid.New()
	payload := mustJSON(t, buildqueue.ImageBuildMessage{BuildID: buildID.String(), ImageID: imageID.String(), OwnerID: ownerID.String(), Tag: "demo:latest"})
	require.NoError(t, repos.Builds.CreateQueuedBuild(ctx,
		model.Image{ID: imageID, OwnerID: ownerID, Tag: "demo:latest", Status: model.ImageStatusBuilding},
		model.Build{ID: buildID, ImageID: imageID, OwnerID: ownerID, Status: model.BuildStatusPending, LogFilePath: "logs", ArchiveObjectKey: "archive"},
		model.BuildQueueOutbox{ID: outboxID, BuildID: buildID, Exchange: buildqueue.ExchangeName, RoutingKey: buildqueue.RoutingKey, Payload: payload},
	))

	publisher := rabbitmq.NewPublisher(url)
	workerCtx, cancel := context.WithCancel(context.Background())
	worker := service.NewBuildOutboxWorker(repos.Builds, publisher, coretest.StaticConfig{Config: coretest.IntegrationConfig()}, coretest.DiscardLogger())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(workerCtx)
	}()
	require.Eventually(t, func() bool {
		var status string
		err := repos.DB.QueryRow(`SELECT status FROM build_queue_outbox WHERE id = $1`, outboxID).Scan(&status)
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

	msg, ok, err := ch.Get(buildqueue.QueueName, true)
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

func purgeQueues(t *testing.T, ch *amqp.Channel) {
	t.Helper()
	for _, queue := range []string{buildqueue.QueueName, composequeue.QueueName, containerqueue.QueueName} {
		_, _ = ch.QueuePurge(queue, false)
	}
}

func declareTopology(t *testing.T, url string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	publisher := rabbitmq.NewPublisher(url)
	require.NoError(t, publisher.Publish(ctx, buildqueue.ExchangeName, buildqueue.RoutingKey, []byte(`{"declare":true}`)))
	require.NoError(t, publisher.Close())
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

func requireQueueEmpty(t *testing.T, ch *amqp.Channel, queue string) {
	t.Helper()
	require.Eventually(t, func() bool {
		msg, ok, err := ch.Get(queue, false)
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
