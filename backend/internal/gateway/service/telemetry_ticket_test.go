package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTelemetryTicketStoreIssueConsumeAndRejectsInvalidTickets(t *testing.T) {
	store := service.NewTelemetryTicketStore(time.Minute)
	ownerID := uuid.New()

	ticket, err := store.Issue(context.Background(), model.TelemetryTicketClaims{
		ContainerID: "container-id",
		StreamType:  model.TelemetryStreamTerminal,
		Scope:       accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID},
	})
	require.NoError(t, err)
	require.NotEmpty(t, ticket.Ticket)

	claims, err := store.Consume(context.Background(), ticket.Ticket, "container-id", model.TelemetryStreamTerminal, false)
	require.NoError(t, err)
	require.Equal(t, ownerID, claims.Scope.UserID)
	_, err = store.Consume(context.Background(), ticket.Ticket, "container-id", model.TelemetryStreamTerminal, false)
	require.ErrorIs(t, err, apperrors.ErrUnauthorized)

	_, err = store.Consume(context.Background(), "bad-ticket", "container-id", model.TelemetryStreamLogs, false)
	require.ErrorIs(t, err, apperrors.ErrUnauthorized)

	ticket, err = store.Issue(context.Background(), model.TelemetryTicketClaims{ContainerID: "container-id", StreamType: model.TelemetryStreamLogs, Scope: accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID}})
	require.NoError(t, err)
	_, err = store.Consume(context.Background(), ticket.Ticket, "other-id", model.TelemetryStreamLogs, false)
	require.ErrorIs(t, err, apperrors.ErrForbidden)

	expiringStore := service.NewTelemetryTicketStore(time.Nanosecond)
	ticket, err = expiringStore.Issue(context.Background(), model.TelemetryTicketClaims{ContainerID: "container-id", StreamType: model.TelemetryStreamLogs, Scope: accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID}})
	require.NoError(t, err)
	time.Sleep(time.Millisecond)
	_, err = expiringStore.Consume(context.Background(), ticket.Ticket, "container-id", model.TelemetryStreamLogs, false)
	require.ErrorIs(t, err, apperrors.ErrUnauthorized)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = store.Issue(ctx, model.TelemetryTicketClaims{})
	require.ErrorIs(t, err, context.Canceled)
	_, err = store.Consume(ctx, ticket.Ticket, "container-id", model.TelemetryStreamLogs, false)
	require.ErrorIs(t, err, context.Canceled)
}
