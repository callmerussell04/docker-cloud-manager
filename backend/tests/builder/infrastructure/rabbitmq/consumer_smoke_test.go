package rabbitmq_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/rabbitmq"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/rabbitmqtopology"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
)

func TestConsumerAcksValidBuildMessageSmoke(t *testing.T) {
	url := os.Getenv("BUILDER_TEST_RABBITMQ_URL")
	if url == "" {
		t.Skip("set BUILDER_TEST_RABBITMQ_URL to run RabbitMQ smoke test")
	}
	ch := openBuildQueueChannel(t, url)
	purgeBuildQueues(t, ch)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handled := make(chan buildqueue.ImageBuildMessage, 1)
	consumer := rabbitmq.NewConsumer(url, "test-ack", slog.Default())
	consumer.Run(ctx, 1, func(ctx context.Context, msg buildqueue.ImageBuildMessage) error {
		handled <- msg
		return nil
	})
	t.Cleanup(func() {
		cancel()
		_ = consumer.Stop(context.Background())
	})

	publishBuildMessage(t, ch, buildqueue.ImageBuildMessage{BuildID: "build-id", RequestID: "request-id"})
	select {
	case msg := <-handled:
		require.Equal(t, "build-id", msg.BuildID)
	case <-time.After(5 * time.Second):
		t.Fatal("build message was not consumed")
	}
	require.Eventually(t, func() bool {
		queue, err := ch.QueueInspect(buildqueue.QueueName)
		require.NoError(t, err)
		return queue.Messages == 0
	}, 5*time.Second, 100*time.Millisecond)
}

func TestConsumerDoesNotRequeuePermanentErrorsSmoke(t *testing.T) {
	url := os.Getenv("BUILDER_TEST_RABBITMQ_URL")
	if url == "" {
		t.Skip("set BUILDER_TEST_RABBITMQ_URL to run RabbitMQ smoke test")
	}
	ch := openBuildQueueChannel(t, url)
	purgeBuildQueues(t, ch)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handled := make(chan struct{}, 1)
	consumer := rabbitmq.NewConsumer(url, "test-nack", slog.Default())
	consumer.Run(ctx, 1, func(ctx context.Context, msg buildqueue.ImageBuildMessage) error {
		handled <- struct{}{}
		return apperrors.ErrBadRequest
	})
	t.Cleanup(func() {
		cancel()
		_ = consumer.Stop(context.Background())
	})

	publishBuildMessage(t, ch, buildqueue.ImageBuildMessage{BuildID: "build-id", RequestID: "request-id"})
	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("build message was not consumed")
	}
	require.Eventually(t, func() bool {
		queue, err := ch.QueueInspect(buildqueue.QueueName)
		require.NoError(t, err)
		return queue.Messages == 0
	}, 5*time.Second, 100*time.Millisecond)
}

func openBuildQueueChannel(t *testing.T, url string) *amqp.Channel {
	t.Helper()
	conn, err := amqp.Dial(url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	ch, err := conn.Channel()
	require.NoError(t, err)
	t.Cleanup(func() { _ = ch.Close() })
	require.NoError(t, rabbitmqtopology.DeclareBuild(ch))
	return ch
}

func purgeBuildQueues(t *testing.T, ch *amqp.Channel) {
	t.Helper()
	_, err := ch.QueuePurge(buildqueue.QueueName, false)
	require.NoError(t, err)
	_, err = ch.QueuePurge(buildqueue.DLQName, false)
	require.NoError(t, err)
}

func publishBuildMessage(t *testing.T, ch *amqp.Channel, msg buildqueue.ImageBuildMessage) {
	t.Helper()
	body, err := json.Marshal(msg)
	require.NoError(t, err)
	require.NoError(t, ch.PublishWithContext(context.Background(), buildqueue.ExchangeName, buildqueue.RoutingKey, false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        body,
	}))
}
