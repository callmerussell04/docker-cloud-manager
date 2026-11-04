package rabbitmq_test

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/infrastructure/rabbitmq"
	"github.com/stretchr/testify/require"
)

func TestPublisherCancelsStalledIO(t *testing.T) {
	for _, stage := range []string{"handshake", "topology", "write", "confirmation"} {
		t.Run(stage, func(t *testing.T) {
			broker := newPublisherBroker(t, stage)
			publisher := rabbitmq.NewPublisher(broker.url, rabbitmq.Topology{})
			t.Cleanup(func() { _ = publisher.Close() })
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var body []byte
			if stage == "write" {
				body = make([]byte, 16<<20)
			}
			result := publishAsync(publisher, ctx, body)
			receive(t, broker.ready)
			select {
			case err := <-result:
				t.Fatalf("stalled publication returned before cancellation: %v", err)
			case <-time.After(30 * time.Millisecond):
			}
			cancel()
			require.ErrorIs(t, receive(t, result), context.Canceled)
			if stage != "write" {
				receive(t, broker.disconnected)
			}
		})
	}
}

func TestPublisherBoundsAttemptsWithoutCallerDeadline(t *testing.T) {
	for _, stage := range []string{"handshake", "topology", "write", "confirmation"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			broker := newPublisherBroker(t, stage)
			publisher := rabbitmq.NewPublisher(broker.url, rabbitmq.Topology{})
			t.Cleanup(func() { _ = publisher.Close() })
			var body []byte
			if stage == "write" {
				body = make([]byte, 16<<20)
			}
			result := publishAsync(publisher, t.Context(), body)
			receive(t, broker.ready)
			select {
			case err := <-result:
				require.ErrorIs(t, err, context.DeadlineExceeded)
			case <-time.After(12 * time.Second):
				t.Fatal("application publication budget was not enforced")
			}
			if stage != "write" {
				receive(t, broker.disconnected)
			}
		})
	}
}

func TestPublisherQueuedCallerCanCancelAndCloseInterruptsActiveAttempt(t *testing.T) {
	broker := newPublisherBroker(t, "handshake")
	publisher := rabbitmq.NewPublisher(broker.url, rabbitmq.Topology{})
	t.Cleanup(func() { _ = publisher.Close() })
	active := publishAsync(publisher, t.Context(), nil)
	receive(t, broker.ready)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, receive(t, publishAsync(publisher, ctx, nil)), context.DeadlineExceeded)
	closed := make(chan error, 1)
	go func() { closed <- publisher.Close() }()
	require.Error(t, receive(t, active))
	require.NoError(t, receive(t, closed))
	require.Error(t, publisher.Publish(t.Context(), "test", "test", nil))
	require.NoError(t, publisher.Close())
	receive(t, broker.disconnected)
}

func TestPublisherCloseInterruptsStalledIO(t *testing.T) {
	for _, stage := range []string{"topology", "write", "confirmation"} {
		t.Run(stage, func(t *testing.T) {
			broker := newPublisherBroker(t, stage)
			publisher := rabbitmq.NewPublisher(broker.url, rabbitmq.Topology{})
			t.Cleanup(func() { _ = publisher.Close() })
			body := []byte("message")
			if stage == "write" {
				body = make([]byte, 16<<20)
			}
			result := publishAsync(publisher, t.Context(), body)
			receive(t, broker.ready)
			closed := make(chan error, 1)
			go func() { closed <- publisher.Close() }()
			require.Error(t, receive(t, result))
			require.NoError(t, receive(t, closed))
		})
	}
}

func TestPublisherLateAckCannotConfirmNextMessage(t *testing.T) {
	broker := newPublisherBroker(t, "confirmation")
	publisher := rabbitmq.NewPublisher(broker.url, rabbitmq.Topology{})
	t.Cleanup(func() { _ = publisher.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	first := publishAsync(publisher, ctx, []byte("first"))
	old := receive(t, broker.publications)
	require.ErrorIs(t, receive(t, first), context.DeadlineExceeded)
	receive(t, broker.disconnected)
	second := publishAsync(publisher, t.Context(), []byte("second"))
	current := receive(t, broker.publications)
	require.True(t, old.conn != current.conn, "interrupted confirm stream must be isolated")
	// Deliver the old confirmation only once the new message is awaiting its ACK.
	_ = old.ack(true)
	select {
	case err := <-second:
		t.Fatalf("publication completed before its own confirmation: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	require.NoError(t, current.ack(true))
	require.NoError(t, receive(t, second))
	// The healthy connection remains reusable and delivery tags advance.
	third := publishAsync(publisher, t.Context(), []byte("third"))
	next := receive(t, broker.publications)
	require.True(t, current.conn == next.conn, "healthy connection should be reused")
	require.NoError(t, next.ack(true))
	require.NoError(t, receive(t, third))
}

func TestPublisherRejectsNackAndReconnects(t *testing.T) {
	broker := newPublisherBroker(t, "confirmation")
	publisher := rabbitmq.NewPublisher(broker.url, rabbitmq.Topology{})
	t.Cleanup(func() { _ = publisher.Close() })
	first := publishAsync(publisher, t.Context(), nil)
	msg := receive(t, broker.publications)
	require.NoError(t, msg.ack(false))
	require.ErrorContains(t, receive(t, first), "rejected")
	second := publishAsync(publisher, t.Context(), nil)
	msg = receive(t, broker.publications)
	require.NoError(t, msg.ack(true))
	require.NoError(t, receive(t, second))
}

func publishAsync(p *rabbitmq.Publisher, ctx context.Context, body []byte) <-chan error {
	result := make(chan error, 1)
	go func() { result <- p.Publish(ctx, "test", "test", body) }()
	return result
}

func receive[T any](t *testing.T, values <-chan T) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("publisher or test broker did not make progress")
	}
	var zero T
	return zero
}

// This wire fixture exercises the real AMQP client, including blocking writes
// and handshakes. Mocking Channel.Publish would hide these cancellation failures.
type publisherBroker struct {
	url          string
	ready        chan struct{}
	disconnected chan struct{}
	publications chan brokerPublication
	stopWrite    chan struct{}
}

type brokerPublication struct {
	conn    net.Conn
	channel uint16
	tag     uint64
}

func (p brokerPublication) ack(ack bool) error {
	method := uint16(80)
	if !ack {
		method = 120
	}
	payload := make([]byte, 9)
	binary.BigEndian.PutUint64(payload, p.tag)
	return writeMethod(p.conn, p.channel, 60, method, payload)
}

func newPublisherBroker(t *testing.T, stall string) *publisherBroker {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	broker := &publisherBroker{
		url:   "amqp://guest:guest@" + listener.Addr().String() + "/",
		ready: make(chan struct{}, 10), disconnected: make(chan struct{}, 10),
		publications: make(chan brokerPublication, 10),
		stopWrite:    make(chan struct{}),
	}
	var workers sync.WaitGroup
	var sockets sync.Map
	acceptedDone := make(chan struct{})
	workers.Go(func() {
		defer close(acceptedDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			sockets.Store(conn, struct{}{})
			workers.Go(func() {
				defer func() { _ = conn.Close() }() // fake broker teardown; the peer may already have closed
				defer sockets.Delete(conn)
				defer func() { broker.disconnected <- struct{}{} }()
				broker.serve(conn, stall)
			})
		}
	})
	t.Cleanup(func() {
		_ = listener.Close()
		<-acceptedDone
		close(broker.stopWrite)
		sockets.Range(func(key, _ any) bool { _ = key.(net.Conn).Close(); return true })
		workers.Wait()
	})
	return broker
}

func (b *publisherBroker) serve(conn net.Conn, stall string) {
	header := make([]byte, 8)
	if _, err := io.ReadFull(conn, header); err != nil {
		return
	}
	if stall == "handshake" {
		b.ready <- struct{}{}
		_, _ = io.Copy(io.Discard, conn)
		return
	}
	// AMQP 0-9-1 connection.start: version, empty properties, PLAIN, en_US.
	if writeMethod(conn, 0, 10, 10, []byte{0, 9, 0, 0, 0, 0, 0, 0, 0, 5, 'P', 'L', 'A', 'I', 'N', 0, 0, 0, 5, 'e', 'n', '_', 'U', 'S'}) != nil {
		return
	}
	var tag uint64
	for {
		frameType, channel, payload, err := readFrame(conn)
		if err != nil {
			return
		}
		if frameType == 3 || (frameType == 2 && len(payload) >= 12 && binary.BigEndian.Uint64(payload[4:12]) == 0) {
			tag++
			b.publications <- brokerPublication{conn: conn, channel: channel, tag: tag}
			b.ready <- struct{}{}
			continue
		}
		if frameType != 1 || len(payload) < 4 {
			continue
		}
		class, method := binary.BigEndian.Uint16(payload), binary.BigEndian.Uint16(payload[2:])
		var response []byte
		var reply uint16
		switch {
		case class == 10 && method == 11: // start-ok -> tune
			reply = 30
			response = []byte{0, 0, 0, 2, 0, 0, 0, 0} // 128 KiB frames; no heartbeat
		case class == 10 && method == 31:
			continue // tune-ok
		case class == 10 && method == 40:
			reply, response = 41, []byte{0}
		case class == 20 && method == 10:
			reply, response = 11, []byte{0, 0, 0, 0}
		case class == 40 && method == 10: // exchange.declare
			if stall == "topology" {
				b.ready <- struct{}{}
				_, _ = io.Copy(io.Discard, conn)
				return
			}
			reply = 11
		case class == 50 && method == 10:
			reply, response = 11, make([]byte, 9)
		case class == 50 && method == 20:
			reply = 21
		case class == 85 && method == 10:
			reply = 11
		case class == 60 && method == 40:
			if stall == "write" {
				b.ready <- struct{}{}
				<-b.stopWrite
				return
			}
			continue // basic.publish, content follows
		default:
			return
		}
		if writeMethod(conn, channel, class, reply, response) != nil {
			return
		}
	}
}

func readFrame(conn net.Conn) (byte, uint16, []byte, error) {
	header := make([]byte, 7)
	if _, err := io.ReadFull(conn, header); err != nil {
		return 0, 0, nil, err
	}
	payload := make([]byte, int(binary.BigEndian.Uint32(header[3:]))+1)
	_, err := io.ReadFull(conn, payload)
	return header[0], binary.BigEndian.Uint16(header[1:]), payload[:len(payload)-1], err
}

func writeMethod(conn net.Conn, channel, class, method uint16, payload []byte) error {
	frame := make([]byte, 11+len(payload)+1)
	frame[0] = 1
	binary.BigEndian.PutUint16(frame[1:], channel)
	binary.BigEndian.PutUint32(frame[3:], uint32(4+len(payload)))
	binary.BigEndian.PutUint16(frame[7:], class)
	binary.BigEndian.PutUint16(frame[9:], method)
	copy(frame[11:], payload)
	frame[len(frame)-1] = 0xce
	_, err := conn.Write(frame)
	return err
}
