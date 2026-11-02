package rabbitmq

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/rabbitmqtopology"
	amqp "github.com/rabbitmq/amqp091-go"
)

// A single budget includes waiting for the publisher, dialing, topology setup,
// writing the message, and waiting for its broker confirmation.
const publishAttemptTimeout = 10 * time.Second

type Publisher struct {
	url      string
	gate     chan struct{}
	lifetime context.Context
	cancel   context.CancelFunc
	socket   net.Conn
	conn     *amqp.Connection
	ch       *amqp.Channel
	confirms <-chan amqp.Confirmation
}

func NewPublisher(url string) *Publisher {
	lifetime, cancel := context.WithCancel(context.Background())
	return &Publisher{url: url, gate: make(chan struct{}, 1), lifetime: lifetime, cancel: cancel}
}

func (p *Publisher) Publish(ctx context.Context, exchange, routingKey string, body []byte) (err error) {
	ctx, cancel := context.WithTimeout(ctx, publishAttemptTimeout)
	defer cancel()
	stopClose := context.AfterFunc(p.lifetime, cancel)
	defer stopClose()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case p.gate <- struct{}{}:
	}
	defer func() { <-p.gate }()
	if p.lifetime.Err() != nil {
		return fmt.Errorf("rabbitmq publisher is closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	stopIO := func() {}
	defer func() {
		// Join the cancellation callback before allowing reuse of a healthy socket.
		stopIO()
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil {
			_ = p.resetLocked() // discards the failed connection; the attempt's error is the one reported
		}
	}()
	if p.socket != nil {
		stopIO = interruptOnCancel(ctx, p.socket)
	}
	if err := p.ensureConnected(ctx, &stopIO); err != nil {
		return err
	}
	if err := p.ch.PublishWithContext(ctx, exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
		Body:         body,
	}); err != nil {
		return fmt.Errorf("failed to publish queue message: %w", err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case confirm, ok := <-p.confirms:
		if !ok {
			return fmt.Errorf("rabbitmq publisher confirms channel closed")
		}
		if !confirm.Ack {
			return fmt.Errorf("rabbitmq rejected queue message")
		}
		return nil
	}
}

// interruptOnCancel bounds synchronous AMQP I/O, including TLS and AMQP
// handshakes, which do not accept a context themselves.
func interruptOnCancel(ctx context.Context, socket net.Conn) func() {
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		_ = socket.Close() // interrupts blocked I/O; the canceled context is reported instead
	})
	return func() {
		if !stop() {
			<-done
		}
	}
}

func (p *Publisher) Close() error {
	// Cancel before acquiring the gate so active I/O and queued callers wake up.
	p.cancel()
	p.gate <- struct{}{}
	defer func() { <-p.gate }()
	return p.resetLocked()
}

func (p *Publisher) ensureConnected(ctx context.Context, stopIO *func()) error {
	if p.conn != nil && !p.conn.IsClosed() && p.ch != nil {
		return nil
	}
	(*stopIO)()
	*stopIO = func() {}
	_ = p.resetLocked() // discards a dead connection before redialing; the dial reports any failure
	conn, err := amqp.DialConfig(p.url, amqp.Config{
		Dial: func(network, addr string) (net.Conn, error) {
			socket, err := (&net.Dialer{}).DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			p.socket = socket
			*stopIO = interruptOnCancel(ctx, socket)
			return socket, nil
		},
	})
	if err != nil {
		return fmt.Errorf("failed to connect to rabbitmq: %w", err)
	}
	p.conn = conn
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("failed to open rabbitmq channel: %w", err)
	}
	p.ch = ch
	if err := declareTopology(ch); err != nil {
		return err
	}
	if err := ch.Confirm(false); err != nil {
		return fmt.Errorf("failed to enable publisher confirms: %w", err)
	}
	p.confirms = ch.NotifyPublish(make(chan amqp.Confirmation, 1))
	return nil
}

func (p *Publisher) resetLocked() error {
	// Closing the transport first avoids an unbounded AMQP close handshake and
	// isolates every failed attempt from late confirmations on its old channel.
	var err error
	if p.socket != nil {
		err = p.socket.Close()
	}
	if p.conn != nil {
		_ = p.conn.CloseDeadline(time.Now()) // the socket is already closed; this only releases AMQP state
	}
	p.socket = nil
	p.conn = nil
	p.ch = nil
	p.confirms = nil
	return err
}

func declareTopology(ch *amqp.Channel) error {
	return rabbitmqtopology.DeclareAll(ch)
}
