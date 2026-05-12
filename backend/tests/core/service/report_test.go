package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	. "github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestReportServiceListMethodsNormalizePagination(t *testing.T) {
	reports := coremocks.NewReportsRepository(t)
	svc := NewReportService(reports, coremocks.NewUsageSnapshotSource(t), coremocks.NewReportUserDirectory(t), discardLogger())
	var userUsageLimit int
	var userUsageOffset int
	var userUsageFrom time.Time
	var userUsageTo time.Time
	var auditFilters model.AuditEventFilters

	reports.EXPECT().
		ListUserUsageReport(mock.Anything, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), "", 20, 0).
		Run(func(ctx context.Context, from, to time.Time, sort string, limit, offset int) {
			userUsageFrom = from
			userUsageTo = to
			userUsageLimit = limit
			userUsageOffset = offset
		}).
		Return([]model.UserUsageReportItem(nil), 7, nil)
	reports.EXPECT().
		ListAuditEvents(mock.Anything, mock.AnythingOfType("model.AuditEventFilters")).
		Run(func(ctx context.Context, filters model.AuditEventFilters) {
			auditFilters = filters
		}).
		Return([]model.AuditEvent(nil), 0, nil)

	_, total, err := svc.ListUserUsageReport(context.Background(), time.Time{}, time.Time{}, "", -1, -10)
	require.NoError(t, err)
	require.Equal(t, 7, total)
	require.Equal(t, 20, userUsageLimit)
	require.Equal(t, 0, userUsageOffset)
	require.True(t, userUsageFrom.Before(userUsageTo))

	_, _, err = svc.ListAuditEvents(context.Background(), model.AuditEventFilters{Limit: 500, Offset: -20})
	require.NoError(t, err)
	require.Equal(t, 20, auditFilters.Limit)
	require.Equal(t, 0, auditFilters.Offset)
	require.True(t, auditFilters.From.Before(auditFilters.To))
}

func TestReportServiceRecordAuditEventFillsDefaultsAndSwallowsWriteErrors(t *testing.T) {
	reports := coremocks.NewReportsRepository(t)
	svc := NewReportService(reports, coremocks.NewUsageSnapshotSource(t), coremocks.NewReportUserDirectory(t), discardLogger())
	ctx := logging.ContextWithRequestID(context.Background(), "req-123")
	var recorded model.AuditEvent

	reports.EXPECT().
		RecordAuditEvent(mock.Anything, mock.AnythingOfType("model.AuditEvent")).
		Run(func(ctx context.Context, event model.AuditEvent) {
			recorded = event
		}).
		Return(errors.New("clickhouse down"))

	err := svc.RecordAuditEvent(ctx, model.AuditEvent{Action: "containers.create", ResourceType: "container"})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, recorded.ID)
	require.False(t, recorded.OccurredAt.IsZero())
	require.Equal(t, auditlog.ActorScopeUnknown, recorded.ActorScope)
	require.Equal(t, auditlog.OutcomeSuccess, recorded.Outcome)
	require.Equal(t, "{}", recorded.DetailsJSON)
	require.Equal(t, "req-123", recorded.RequestID)
}

func TestReportServiceCollectUsageSnapshotEnrichesUsernames(t *testing.T) {
	ownerID := uuid.New()
	at := time.Date(2026, 5, 12, 14, 23, 0, 0, time.UTC)
	snapshots := []model.UsageSnapshot{{
		OwnerID:     ownerID,
		BucketStart: at.Truncate(time.Hour),
		CollectedAt: at,
	}}
	reports := coremocks.NewReportsRepository(t)
	source := coremocks.NewUsageSnapshotSource(t)
	users := coremocks.NewReportUserDirectory(t)
	svc := NewReportService(reports, source, users, discardLogger())
	var inserted []model.UsageSnapshot

	source.EXPECT().
		CollectUsageSnapshots(mock.Anything, at.Truncate(time.Hour), at).
		Return(snapshots, nil)
	users.EXPECT().
		GetUsers(mock.Anything, []uuid.UUID{ownerID}).
		Return(map[uuid.UUID]model.UserInfo{ownerID: {ID: ownerID, Username: "alice"}}, nil)
	reports.EXPECT().
		InsertUsageSnapshots(mock.Anything, mock.AnythingOfType("[]model.UsageSnapshot")).
		Run(func(ctx context.Context, got []model.UsageSnapshot) {
			inserted = got
		}).
		Return(nil)

	require.NoError(t, svc.CollectUsageSnapshot(context.Background(), at))
	require.Len(t, inserted, 1)
	require.Equal(t, "alice", inserted[0].OwnerUsername)
	require.Equal(t, at.Truncate(time.Hour), inserted[0].BucketStart)
	require.Equal(t, at, inserted[0].CollectedAt)
}

func TestReportServiceGetUserUsageTimelineAppendsLiveCurrentHourSnapshot(t *testing.T) {
	ownerID := uuid.New()
	currentHour := time.Now().UTC().Truncate(time.Hour)
	reports := coremocks.NewReportsRepository(t)
	source := coremocks.NewUsageSnapshotSource(t)
	users := coremocks.NewReportUserDirectory(t)
	svc := NewReportService(reports, source, users, discardLogger())

	reports.EXPECT().
		GetUserUsageTimeline(mock.Anything, ownerID, currentHour.Add(-time.Hour), currentHour.Add(time.Hour)).
		Return([]model.UserUsagePoint{}, nil)
	source.EXPECT().
		CollectUsageSnapshots(mock.Anything, currentHour, mock.AnythingOfType("time.Time")).
		Return([]model.UsageSnapshot{{
			OwnerID:             ownerID,
			BucketStart:         currentHour,
			ReservedMemoryBytes: 128,
			TotalDiskBytes:      256,
			ContainersTotal:     3,
			ContainersRunning:   1,
		}}, nil)
	users.EXPECT().GetUsers(mock.Anything, []uuid.UUID{ownerID}).Return(map[uuid.UUID]model.UserInfo{}, nil)

	points, err := svc.GetUserUsageTimeline(context.Background(), ownerID, currentHour.Add(-time.Hour), currentHour.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, points, 1)
	require.Equal(t, currentHour, points[0].BucketStart)
	require.EqualValues(t, 128, points[0].ReservedMemoryBytes)
	require.EqualValues(t, 256, points[0].TotalDiskBytes)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
