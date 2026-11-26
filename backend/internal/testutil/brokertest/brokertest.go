// Package brokertest gives integration tests RabbitMQ names of their own, so
// test packages can share one broker and run in parallel.
package brokertest

import (
	"testing"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/rabbitmq"
)

// UniqueQueue returns base with every exchange and queue name made unique to t. The
// routing keys are unchanged. The renamed topology is deleted from the broker
// at url when t ends.
func UniqueQueue(t testing.TB, url string, base rabbitmq.Queue) rabbitmq.Queue {
	t.Helper()
	suffix := ".test." + uuid.NewString()
	q := base
	q.Exchange += suffix
	q.Name += suffix
	q.DLX += suffix
	q.DLQ += suffix
	t.Cleanup(func() {
		// Best effort: the names are unique to t and the test broker is disposable.
		conn, err := amqp.Dial(url)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		ch, err := conn.Channel()
		if err != nil {
			return
		}
		_, _ = ch.QueueDelete(q.Name, false, false, false)
		_, _ = ch.QueueDelete(q.DLQ, false, false, false)
		_ = ch.ExchangeDelete(q.Exchange, false, false)
		_ = ch.ExchangeDelete(q.DLX, false, false)
	})
	return q
}
