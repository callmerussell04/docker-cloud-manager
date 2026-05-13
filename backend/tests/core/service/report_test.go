package service_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	coreconfig "github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	. "github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestReportServiceListMethodsNormalizePagination(t *testing.T) {
	reports := coremocks.NewReportsRepository(t)
	svc := NewReportService(reports, coremocks.NewUsageSnapshotSource(t), coremocks.NewReportUserDirectory(t), testReportConfig(), discardLogger())
	var userUsageLimit int
	var userUsageOffset int
	var userUsageFrom time.Time
	var userUsageTo time.Time
	var auditFilters model.AuditEventFilters

	reports.EXPECT().
		ListUserUsageReport(mock.Anything, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), "", "", 20, 0).
		Run(func(ctx context.Context, from, to time.Time, sort, search string, limit, offset int) {
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

	_, total, err := svc.ListUserUsageReport(context.Background(), time.Time{}, time.Time{}, "", "", -1, -10)
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

func TestReportServiceReadErrorsReturnUnavailable(t *testing.T) {
	reports := coremocks.NewReportsRepository(t)
	var logs bytes.Buffer
	svc := NewReportService(reports, coremocks.NewUsageSnapshotSource(t), coremocks.NewReportUserDirectory(t), testReportConfig(), slog.New(slog.NewTextHandler(&logs, nil)))

	reports.EXPECT().
		ListAuditEvents(mock.Anything, mock.AnythingOfType("model.AuditEventFilters")).
		Return(nil, 0, errors.New("clickhouse illegal aggregation"))

	_, _, err := svc.ListAuditEvents(context.Background(), model.AuditEventFilters{Limit: 20})
	require.ErrorIs(t, err, apperrors.ErrUnavailable)
	require.Equal(t, apperrors.ErrUnavailable.Error(), apperrors.SafeMessage(err))
	require.True(t, strings.Contains(logs.String(), "list_audit_events"))
	require.True(t, strings.Contains(logs.String(), "clickhouse illegal aggregation"))
}

func TestReportServiceRecordAuditEventFillsDefaultsAndSwallowsWriteErrors(t *testing.T) {
	reports := coremocks.NewReportsRepository(t)
	svc := NewReportService(reports, coremocks.NewUsageSnapshotSource(t), coremocks.NewReportUserDirectory(t), testReportConfig(), discardLogger())
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
	bucketStart := time.Date(2026, 5, 12, 14, 20, 0, 0, time.UTC)
	snapshots := []model.UsageSnapshot{{
		OwnerID:     ownerID,
		BucketStart: bucketStart,
		CollectedAt: at,
	}}
	reports := coremocks.NewReportsRepository(t)
	source := coremocks.NewUsageSnapshotSource(t)
	users := coremocks.NewReportUserDirectory(t)
	svc := NewReportService(reports, source, users, testReportConfig(), discardLogger())
	var inserted []model.UsageSnapshot

	source.EXPECT().
		CollectUsageSnapshots(mock.Anything, bucketStart, at).
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

	collection, err := svc.CollectUsageSnapshot(context.Background(), at)
	require.NoError(t, err)
	require.Len(t, inserted, 1)
	require.Equal(t, "alice", inserted[0].OwnerUsername)
	require.Equal(t, bucketStart, inserted[0].BucketStart)
	require.Equal(t, at, inserted[0].CollectedAt)
	require.Equal(t, bucketStart, collection.BucketStart)
	require.Equal(t, at, collection.CollectedAt)
	require.Equal(t, 1, collection.SnapshotsCount)
}

func TestReportServiceGetUserUsageTimelineUsesConfiguredBucketInterval(t *testing.T) {
	ownerID := uuid.New()
	from := time.Date(2026, 5, 12, 14, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	reports := coremocks.NewReportsRepository(t)
	svc := NewReportService(reports, coremocks.NewUsageSnapshotSource(t), coremocks.NewReportUserDirectory(t), testReportConfig(), discardLogger())
	var gotInterval time.Duration

	reports.EXPECT().
		GetUserUsageTimeline(mock.Anything, ownerID, from, to, 5*time.Minute).
		Run(func(ctx context.Context, gotOwnerID uuid.UUID, gotFrom, gotTo time.Time, bucketInterval time.Duration) {
			gotInterval = bucketInterval
		}).
		Return([]model.UserUsagePoint{{BucketStart: from}}, nil)

	points, err := svc.GetUserUsageTimeline(context.Background(), ownerID, from, to)
	require.NoError(t, err)
	require.Len(t, points, 1)
	require.Equal(t, 5*time.Minute, gotInterval)
}

type staticReportConfig struct{}

func (staticReportConfig) Get() coreconfig.SystemConfig {
	return coreconfig.SystemConfig{ReportsUsageSnapshotIntervalSeconds: 300}
}

func testReportConfig() staticReportConfig {
	return staticReportConfig{}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
