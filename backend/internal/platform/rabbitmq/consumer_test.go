package rabbitmq_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/rabbitmq"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/testenv"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
)

var errPermanent = errors.New("permanent test failure")

type testMessage struct {
	ID        string `json:"id"`
	RequestID string `json:"request_id"`
}

type harness struct {
	url   string
	queue rabbitmq.Queue
	ch    *amqp.Channel
}

// newHarness declares a queue unique to the test, so these tests never touch a
// workload's queue.
func newHarness(t *testing.T) *harness {
	t.Helper()
	url := testenv.Lookup(t, "CORE_TEST_RABBITMQ_URL")
	id := "dcm.test." + uuid.NewString()
	h := &harness{url: url, queue: rabbitmq.Queue{
		Label:      "test",
		Exchange:   id,
		Name:       id + ".queue",
		RoutingKey: "work",
		DLX:        id + ".dlx",
		DLQ:        id + ".queue.dlq",
		DLQKey:     "work.dead",
	}}
	conn, err := amqp.Dial(url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() }) // test teardown; broker close failure is not actionable
	h.ch, err = conn.Channel()
	require.NoError(t, err)
	require.NoError(t, rabbitmq.Declare(h.ch, h.queue))
	t.Cleanup(func() {
		// A fresh channel, because a test may have closed h.ch with a failed declare.
		ch, err := conn.Channel()
		if err != nil {
			return
		}
		// Best effort; the names are unique to this test and the broker is disposable.
		_, _ = ch.QueueDelete(h.queue.Name, false, false, false)
		_, _ = ch.QueueDelete(h.queue.DLQ, false, false, false)
		_ = ch.ExchangeDelete(h.queue.Exchange, false, false)
		_ = ch.ExchangeDelete(h.queue.DLX, false, false)
	})
	return h
}

// run starts one worker and returns a stop function that joins it. Cleanup
// stops it too, so a failing test cannot leak the worker.
func (h *harness) run(t *testing.T, handler func(context.Context, testMessage) error) func() {
	t.Helper()
	return h.runWithRetryDelay(t, time.Millisecond, handler)
}

// runWithRetryDelay is run with the wait before a failed message is returned.
func (h *harness) runWithRetryDelay(t *testing.T, retryDelay time.Duration, handler func(context.Context, testMessage) error) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	consumer := rabbitmq.NewConsumer(rabbitmq.ConsumerConfig[testMessage]{
		URL:         h.url,
		Queue:       h.queue.Name,
		ConsumerTag: func(workerID int) string { return h.queue.Name + "-consumer" },
		Declare:     func(ch *amqp.Channel) error { return rabbitmq.Declare(ch, h.queue) },
		IsPermanent: func(err error) bool { return errors.Is(err, errPermanent) },
		RequestID:   func(msg testMessage) string { return msg.RequestID },
		LogFields:   func(msg testMessage) []any { return []any{"message_id", msg.ID} },
		Logger:      slog.New(slog.DiscardHandler),
		RetryDelay:  retryDelay,
	})
	consumer.Run(ctx, 1, handler)
	stop := func() {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		require.NoError(t, consumer.Stop(stopCtx))
	}
	t.Cleanup(stop)
	return stop
}

func (h *harness) publish(t *testing.T, body []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, h.ch.PublishWithContext(ctx, h.queue.Exchange, h.queue.RoutingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	}))
}

func (h *harness) publishMessage(t *testing.T, msg testMessage) {
	t.Helper()
	body, err := json.Marshal(msg)
	require.NoError(t, err)
	h.publish(t, body)
}

func (h *harness) requireDeadLetter(t *testing.T, want string) {
	t.Helper()
	var delivery amqp.Delivery
	require.Eventually(t, func() bool {
		msg, ok, err := h.ch.Get(h.queue.DLQ, true)
		delivery = msg
		return err == nil && ok
	}, 5*time.Second, 50*time.Millisecond, "message was not dead-lettered")
	require.Equal(t, want, string(delivery.Body))
	require.Equal(t, "application/json", delivery.ContentType)
	require.Equal(t, uint8(amqp.Persistent), delivery.DeliveryMode)
}

func (h *harness) requireNoMessage(t *testing.T, queue string) {
	t.Helper()
	_, ok, err := h.ch.Get(queue, true)
	require.NoError(t, err)
	require.False(t, ok, "queue %s must be empty", queue)
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for delivery")
		panic("unreachable")
	}
}

func TestConsumerAcksHandledMessage(t *testing.T) {
	h := newHarness(t)
	handled := make(chan testMessage, 1)
	requestIDs := make(chan string, 1)
	stop := h.run(t, func(ctx context.Context, msg testMessage) error {
		requestIDs <- logging.RequestIDFromContext(ctx)
		handled <- msg
		return nil
	})

	want := testMessage{ID: "ok", RequestID: "request-1"}
	h.publishMessage(t, want)

	require.Equal(t, want, receive(t, handled))
	require.Equal(t, "request-1", receive(t, requestIDs))
	// Closing the consumer connection waits for its preceding ACK to reach the broker.
	stop()
	h.requireNoMessage(t, h.queue.Name)
	h.requireNoMessage(t, h.queue.DLQ)
}

func TestConsumerDeadLettersBadMessageWithoutCallingHandler(t *testing.T) {
	h := newHarness(t)
	var calls atomic.Int32
	h.run(t, func(context.Context, testMessage) error {
		calls.Add(1)
		return nil
	})

	h.publish(t, []byte(`not-json`))

	h.requireDeadLetter(t, `not-json`)
	require.Zero(t, calls.Load())
}

func TestConsumerDeadLettersPermanentError(t *testing.T) {
	h := newHarness(t)
	var calls atomic.Int32
	h.run(t, func(context.Context, testMessage) error {
		calls.Add(1)
		return errors.Join(errors.New("context"), errPermanent)
	})

	h.publishMessage(t, testMessage{ID: "permanent"})

	h.requireDeadLetter(t, `{"id":"permanent","request_id":""}`)
	require.EqualValues(t, 1, calls.Load())
}

func TestConsumerRequeuesTransientError(t *testing.T) {
	h := newHarness(t)
	var attempts atomic.Int32
	retried := make(chan testMessage, 1)
	stop := h.run(t, func(_ context.Context, msg testMessage) error {
		if attempts.Add(1) == 1 {
			return errors.New("temporary downstream failure")
		}
		retried <- msg
		return nil
	})

	want := testMessage{ID: "transient"}
	h.publishMessage(t, want)

	require.Equal(t, want, receive(t, retried))
	stop()
	require.EqualValues(t, 2, attempts.Load())
	h.requireNoMessage(t, h.queue.Name)
	h.requireNoMessage(t, h.queue.DLQ)
}

func TestConsumerDeadLettersPermanentErrorWithoutRetryDelay(t *testing.T) {
	h := newHarness(t)
	h.runWithRetryDelay(t, time.Minute, func(context.Context, testMessage) error {
		return errPermanent
	})

	h.publishMessage(t, testMessage{ID: "permanent"})

	h.requireDeadLetter(t, `{"id":"permanent","request_id":""}`)
}

// A worker holds a retryably failed delivery for the retry delay instead of
// returning it at once, and shutdown returns it without waiting the delay out.
func TestConsumerHoldsTransientErrorForTheRetryDelayAndReturnsItOnStop(t *testing.T) {
	h := newHarness(t)
	var attempts atomic.Int32
	failed := make(chan struct{}, 1)
	stop := h.runWithRetryDelay(t, time.Minute, func(context.Context, testMessage) error {
		attempts.Add(1)
		failed <- struct{}{}
		return errors.New("temporary downstream failure")
	})

	h.publishMessage(t, testMessage{ID: "held"})
	receive(t, failed)

	require.Never(t, func() bool { return attempts.Load() > 1 }, 300*time.Millisecond, 50*time.Millisecond, "the delivery is held, not redelivered")
	h.requireNoMessage(t, h.queue.Name)
	// stop fails the test unless the worker exits well within the delay.
	stop()
	require.Eventually(t, func() bool {
		msg, ok, err := h.ch.Get(h.queue.Name, true)
		return err == nil && ok && string(msg.Body) == `{"id":"held","request_id":""}`
	}, 5*time.Second, 50*time.Millisecond, "the held delivery must return to the queue")
	h.requireNoMessage(t, h.queue.DLQ)
}

// RabbitMQ 4 counts only deliveries returned by a closed channel or
// connection against a quorum queue's delivery limit of 20, and dead-letters
// the message on the next return. This is how a queued operation loses its
// message: a worker that keeps dying or losing its connection mid-message.
func TestQueueDeadLettersMessageAbandonedPastTheDeliveryLimit(t *testing.T) {
	h := newHarness(t)
	conn, err := amqp.Dial(h.url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() }) // test teardown; broker close failure is not actionable

	h.publishMessage(t, testMessage{ID: "lost"})
	for range 21 {
		ch, err := conn.Channel()
		require.NoError(t, err)
		require.NoError(t, ch.Qos(1, 0, false))
		deliveries, err := ch.Consume(h.queue.Name, "", false, false, false, false, nil)
		require.NoError(t, err)
		receive(t, deliveries)
		// Closing the channel with the delivery unacknowledged abandons it.
		require.NoError(t, ch.Close())
	}

	h.requireDeadLetter(t, `{"id":"lost","request_id":""}`)
}

// A retryable failure is returned with a requeue, which RabbitMQ 4 does not
// count against the delivery limit: the message is never dead-lettered by
// failing, and only the retry delay spaces its attempts.
func TestConsumerNeverDeadLettersRepeatedTransientError(t *testing.T) {
	h := newHarness(t)
	var attempts atomic.Int32
	h.run(t, func(context.Context, testMessage) error {
		attempts.Add(1)
		return errors.New("database unreachable")
	})

	h.publishMessage(t, testMessage{ID: "kept"})

	require.Eventually(t, func() bool { return attempts.Load() > 25 }, 10*time.Second, 10*time.Millisecond)
	h.requireNoMessage(t, h.queue.DLQ)
}

func TestConsumerHoldsOneUnacknowledgedMessagePerWorker(t *testing.T) {
	h := newHarness(t)
	handled := make(chan testMessage, 2)
	release := make(chan struct{})
	h.run(t, func(ctx context.Context, msg testMessage) error {
		handled <- msg
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	})

	h.publishMessage(t, testMessage{ID: "first"})
	h.publishMessage(t, testMessage{ID: "second"})

	require.Equal(t, "first", receive(t, handled).ID)
	require.Never(t, func() bool { return len(handled) > 0 }, 300*time.Millisecond, 50*time.Millisecond)
	close(release)
	require.Equal(t, "second", receive(t, handled).ID)
}

// Deleting the queue makes the broker cancel the consumer, which closes its
// delivery channel; the worker must reconnect and declare the topology again.
func TestConsumerReconnectsAndRedeclaresAfterDeliveriesClose(t *testing.T) {
	h := newHarness(t)
	handled := make(chan testMessage, 2)
	h.run(t, func(_ context.Context, msg testMessage) error {
		handled <- msg
		return nil
	})
	h.publishMessage(t, testMessage{ID: "before"})
	require.Equal(t, "before", receive(t, handled).ID)

	_, err := h.ch.QueueDelete(h.queue.Name, false, false, false)
	require.NoError(t, err)

	// A publish before the worker rebinds the queue would be dropped by the exchange.
	require.Eventually(t, h.queueHasConsumer(t), 15*time.Second, 100*time.Millisecond)
	h.publishMessage(t, testMessage{ID: "after"})
	require.Equal(t, "after", receive(t, handled).ID)
}

// queueHasConsumer opens a fresh channel per check because a passive declare of
// an absent queue closes the channel it runs on.
func (h *harness) queueHasConsumer(t *testing.T) func() bool {
	t.Helper()
	conn, err := amqp.Dial(h.url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() }) // test teardown; broker close failure is not actionable
	return func() bool {
		ch, err := conn.Channel()
		if err != nil {
			return false
		}
		defer func() { _ = ch.Close() }() // test probe; a failed close only ends this check
		info, err := ch.QueueDeclarePassive(h.queue.Name, true, false, false, false, nil)
		return err == nil && info.Consumers == 1
	}
}

func TestDeclareRoutesRejectedMessagesToDeadLetterQueue(t *testing.T) {
	h := newHarness(t)

	h.publish(t, []byte(`routed`))
	var delivery amqp.Delivery
	require.Eventually(t, func() bool {
		msg, ok, err := h.ch.Get(h.queue.Name, false)
		delivery = msg
		return err == nil && ok
	}, 5*time.Second, 50*time.Millisecond)
	require.NoError(t, delivery.Nack(false, false))

	h.requireDeadLetter(t, `routed`)
	// Declaring the same description again must match the existing queue arguments.
	require.NoError(t, rabbitmq.Declare(h.ch, h.queue))
}

func TestDeclareWrapsFailureWithQueueLabel(t *testing.T) {
	h := newHarness(t)
	conflicting := h.queue
	conflicting.Label = "conflict"
	conflicting.DLQKey = "another.key"

	err := rabbitmq.Declare(h.ch, conflicting)

	require.ErrorContains(t, err, "failed to declare conflict queue")
}
