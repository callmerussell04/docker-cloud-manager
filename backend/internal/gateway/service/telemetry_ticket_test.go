package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

func TestTelemetryTicketStoreIssueConsumeOnce(t *testing.T) {
	store := NewTelemetryTicketStore(time.Minute)
	ownerID := uuid.New()
	ticket, err := store.Issue(context.Background(), model.TelemetryTicketClaims{
		ContainerID: "container-id",
		StreamType:  model.TelemetryStreamTerminal,
		Scope:       accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID},
	})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	claims, err := store.Consume(context.Background(), ticket.Ticket, "container-id", model.TelemetryStreamTerminal, false)
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if claims.Scope.UserID != ownerID {
		t.Fatalf("claims user_id = %s, want %s", claims.Scope.UserID, ownerID)
	}

	_, err = store.Consume(context.Background(), ticket.Ticket, "container-id", model.TelemetryStreamTerminal, false)
	if !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("second Consume() error = %v, want ErrUnauthorized", err)
	}
}

func TestTelemetryTicketStoreRejectsExpiredAndMismatchedTickets(t *testing.T) {
	now := time.Now()
	store := NewTelemetryTicketStore(time.Minute)
	store.now = func() time.Time { return now }
	if _, err := store.Consume(context.Background(), "not-a-ticket", "container-id", model.TelemetryStreamLogs, false); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("malformed Consume() error = %v, want ErrUnauthorized", err)
	}

	ticket, err := store.Issue(context.Background(), model.TelemetryTicketClaims{
		ContainerID: "container-id",
		StreamType:  model.TelemetryStreamLogs,
		Scope:       accessscope.Scope{Kind: accessscope.KindUser, UserID: uuid.New()},
	})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	if _, err := store.Consume(context.Background(), ticket.Ticket, "other-id", model.TelemetryStreamLogs, false); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatalf("wrong container Consume() error = %v, want ErrForbidden", err)
	}

	ticket, err = store.Issue(context.Background(), model.TelemetryTicketClaims{
		ContainerID: "container-id",
		StreamType:  model.TelemetryStreamLogs,
		Scope:       accessscope.Scope{Kind: accessscope.KindUser, UserID: uuid.New()},
	})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	store.now = func() time.Time { return now.Add(2 * time.Minute) }
	if _, err := store.Consume(context.Background(), ticket.Ticket, "container-id", model.TelemetryStreamLogs, false); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("expired Consume() error = %v, want ErrUnauthorized", err)
	}

	ticket, err = store.Issue(context.Background(), model.TelemetryTicketClaims{
		ContainerID: "container-id",
		StreamType:  model.TelemetryStreamTerminal,
		AdminRoute:  true,
		Scope:       accessscope.Scope{Kind: accessscope.KindAdmin, UserID: uuid.New()},
	})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if _, err := store.Consume(context.Background(), ticket.Ticket, "container-id", model.TelemetryStreamTerminal, false); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatalf("wrong route Consume() error = %v, want ErrForbidden", err)
	}
}
