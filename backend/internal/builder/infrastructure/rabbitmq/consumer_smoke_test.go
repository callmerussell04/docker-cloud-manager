package rabbitmq_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/infrastructure/rabbitmq"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/buildqueue"
	platformrabbitmq "github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/rabbitmq"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/brokertest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/testenv"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
)

func TestConsumerAcksValidBuildMessageSmoke(t *testing.T) {
	url := testenv.Lookup(t, "BUILDER_TEST_RABBITMQ_URL")
	queue := brokertest.UniqueQueue(t, url, buildqueue.Queue)
	ch := openBuildQueueChannel(t, url, queue)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handled := make(chan buildqueue.ImageBuildMessage, 1)
	consumer := platformrabbitmq.NewConsumer(rabbitmq.BuildConsumerConfig(url, "test-ack", queue, slog.Default()))
	consumer.Run(ctx, 1, func(ctx context.Context, msg buildqueue.ImageBuildMessage) error {
		handled <- msg
		return nil
	})
	t.Cleanup(func() {
		cancel()
		_ = consumer.Stop(context.Background())
	})

	publishBuildMessage(t, ch, queue, buildqueue.ImageBuildMessage{BuildID: "build-id", RequestID: "request-id"})
	select {
	case msg := <-handled:
		require.Equal(t, "build-id", msg.BuildID)
	case <-time.After(5 * time.Second):
		t.Fatal("build message was not consumed")
	}
	require.Eventually(t, func() bool {
		queue, err := ch.QueueDeclarePassive(queue.Name, true, false, false, false, nil)
		require.NoError(t, err)
		return queue.Messages == 0
	}, 5*time.Second, 100*time.Millisecond)
}

func TestConsumerDoesNotRequeuePermanentErrorsSmoke(t *testing.T) {
	url := testenv.Lookup(t, "BUILDER_TEST_RABBITMQ_URL")
	queue := brokertest.UniqueQueue(t, url, buildqueue.Queue)
	ch := openBuildQueueChannel(t, url, queue)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handled := make(chan struct{}, 1)
	consumer := platformrabbitmq.NewConsumer(rabbitmq.BuildConsumerConfig(url, "test-nack", queue, slog.Default()))
	consumer.Run(ctx, 1, func(ctx context.Context, msg buildqueue.ImageBuildMessage) error {
		handled <- struct{}{}
		return apperrors.ErrBadRequest
	})
	t.Cleanup(func() {
		cancel()
		_ = consumer.Stop(context.Background())
	})

	publishBuildMessage(t, ch, queue, buildqueue.ImageBuildMessage{BuildID: "build-id", RequestID: "request-id"})
	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("build message was not consumed")
	}
	require.Eventually(t, func() bool {
		queue, err := ch.QueueDeclarePassive(queue.Name, true, false, false, false, nil)
		require.NoError(t, err)
		return queue.Messages == 0
	}, 5*time.Second, 100*time.Millisecond)
}

func openBuildQueueChannel(t *testing.T, url string, queue platformrabbitmq.Queue) *amqp.Channel {
	t.Helper()
	conn, err := amqp.Dial(url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	ch, err := conn.Channel()
	require.NoError(t, err)
	t.Cleanup(func() { _ = ch.Close() })
	require.NoError(t, platformrabbitmq.Declare(ch, queue))
	return ch
}

func publishBuildMessage(t *testing.T, ch *amqp.Channel, queue platformrabbitmq.Queue, msg buildqueue.ImageBuildMessage) {
	t.Helper()
	body, err := json.Marshal(msg)
	require.NoError(t, err)
	require.NoError(t, ch.PublishWithContext(context.Background(), queue.Exchange, queue.RoutingKey, false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        body,
	}))
}
