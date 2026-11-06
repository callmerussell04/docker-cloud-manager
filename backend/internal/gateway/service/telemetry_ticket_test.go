package service_test

import (
	"context"
	"testing"
	"testing/synctest"
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

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = store.Issue(ctx, model.TelemetryTicketClaims{})
	require.ErrorIs(t, err, context.Canceled)
	_, err = store.Consume(ctx, ticket.Ticket, "container-id", model.TelemetryStreamLogs, false)
	require.ErrorIs(t, err, context.Canceled)
}

func TestTelemetryTicketStoreExpiresTicketsAtTheirTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := service.NewTelemetryTicketStore(time.Minute)
		claims := model.TelemetryTicketClaims{ContainerID: "container-id", StreamType: model.TelemetryStreamLogs, Scope: accessscope.Scope{Kind: accessscope.KindUser, UserID: uuid.New()}}
		live, err := store.Issue(t.Context(), claims)
		require.NoError(t, err)
		expiring, err := store.Issue(t.Context(), claims)
		require.NoError(t, err)

		time.Sleep(time.Minute - time.Nanosecond)
		_, err = store.Consume(t.Context(), live.Ticket, "container-id", model.TelemetryStreamLogs, false)
		require.NoError(t, err, "a ticket is valid until its TTL elapses")

		time.Sleep(time.Nanosecond)
		_, err = store.Consume(t.Context(), expiring.Ticket, "container-id", model.TelemetryStreamLogs, false)
		require.ErrorIs(t, err, apperrors.ErrUnauthorized)
	})
}
