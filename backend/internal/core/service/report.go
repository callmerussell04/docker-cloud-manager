package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/google/uuid"
)

type ReportsRepository interface {
	RecordAuditEvent(ctx context.Context, event model.AuditEvent) error
	InsertUsageSnapshots(ctx context.Context, snapshots []model.UsageSnapshot) error
	GetLatestUsageSnapshotCollectedAt(ctx context.Context) (*time.Time, error)
	GetReportsOverview(ctx context.Context, from, to time.Time) (model.ReportsOverview, error)
	ListUserUsageReport(ctx context.Context, from, to time.Time, sort string, limit, offset int) ([]model.UserUsageReportItem, int, error)
	GetUserUsageTimeline(ctx context.Context, ownerID uuid.UUID, from, to time.Time, bucketInterval time.Duration) ([]model.UserUsagePoint, error)
	ListAuditEvents(ctx context.Context, filters model.AuditEventFilters) ([]model.AuditEvent, int, error)
}

type UsageSnapshotSource interface {
	CollectUsageSnapshots(ctx context.Context, bucketStart, collectedAt time.Time) ([]model.UsageSnapshot, error)
}

type ReportUserDirectory interface {
	GetUsers(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]model.UserInfo, error)
}

type ReportConfigProvider interface {
	Get() config.SystemConfig
}

type ReportService struct {
	reports ReportsRepository
	source  UsageSnapshotSource
	users   ReportUserDirectory
	config  ReportConfigProvider
	logger  *slog.Logger
	now     func() time.Time
	timeout time.Duration
}

func NewReportService(reports ReportsRepository, source UsageSnapshotSource, users ReportUserDirectory, config ReportConfigProvider, logger *slog.Logger) *ReportService {
	return &ReportService{
		reports: reports,
		source:  source,
		users:   users,
		config:  config,
		logger:  logging.WithComponent(logger, "reports"),
		now:     time.Now,
		timeout: 2 * time.Second,
	}
}

func (s *ReportService) GetReportsOverview(ctx context.Context, from, to time.Time) (model.ReportsOverview, error) {
	from, to = normalizeFromTo(from, to, s.now())
	overview, err := s.reports.GetReportsOverview(ctx, from, to)
	if err != nil {
		return model.ReportsOverview{}, reportStorageUnavailable("failed to get reports overview", err)
	}
	lastSnapshotAt, err := s.reports.GetLatestUsageSnapshotCollectedAt(ctx)
	if err != nil {
		return model.ReportsOverview{}, reportStorageUnavailable("failed to get latest usage snapshot timestamp", err)
	}
	overview.LastUsageSnapshotAt = lastSnapshotAt
	overview.UsageSnapshotIntervalSeconds = int64(s.usageSnapshotInterval() / time.Second)
	return overview, nil
}

func (s *ReportService) ListUserUsageReport(ctx context.Context, from, to time.Time, sort string, limit, offset int) ([]model.UserUsageReportItem, int, error) {
	from, to = normalizeFromTo(from, to, s.now())
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	items, total, err := s.reports.ListUserUsageReport(ctx, from, to, sort, limit, offset)
	if err != nil {
		return nil, 0, reportStorageUnavailable("failed to list user usage report", err)
	}
	return items, total, nil
}

func (s *ReportService) GetUserUsageTimeline(ctx context.Context, ownerID uuid.UUID, from, to time.Time) ([]model.UserUsagePoint, error) {
	from, to = normalizeFromTo(from, to, s.now())
	points, err := s.reports.GetUserUsageTimeline(ctx, ownerID, from, to, s.usageSnapshotInterval())
	if err != nil {
		return nil, reportStorageUnavailable("failed to get user usage timeline", err)
	}
	return points, nil
}

func (s *ReportService) ListAuditEvents(ctx context.Context, filters model.AuditEventFilters) ([]model.AuditEvent, int, error) {
	filters.From, filters.To = normalizeFromTo(filters.From, filters.To, s.now())
	if filters.Limit <= 0 || filters.Limit > 100 {
		filters.Limit = 20
	}
	if filters.Offset < 0 {
		filters.Offset = 0
	}
	events, total, err := s.reports.ListAuditEvents(ctx, filters)
	if err != nil {
		return nil, 0, reportStorageUnavailable("failed to list audit events", err)
	}
	return events, total, nil
}

func (s *ReportService) RecordAuditEvent(ctx context.Context, event model.AuditEvent) error {
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = s.now().UTC()
	}
	if event.ActorScope == "" {
		event.ActorScope = auditlog.ActorScopeUnknown
	}
	if event.Outcome == "" {
		event.Outcome = auditlog.OutcomeSuccess
	}
	if event.DetailsJSON == "" {
		event.DetailsJSON = "{}"
	}
	if event.RequestID == "" {
		event.RequestID = logging.RequestIDFromContext(ctx)
	}

	writeCtx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	if err := s.reports.RecordAuditEvent(writeCtx, event); err != nil {
		s.logger.Warn("failed to record audit event", "action", event.Action, "resource_type", event.ResourceType, "error", err)
	}
	return nil
}

func (s *ReportService) CollectUsageSnapshot(ctx context.Context, at time.Time) (model.UsageSnapshotCollection, error) {
	interval := s.usageSnapshotInterval()
	bucketStart := at.UTC().Truncate(interval)
	collectedAt := at.UTC()
	snapshots, err := s.collectSnapshots(ctx, bucketStart, collectedAt)
	if err != nil {
		return model.UsageSnapshotCollection{}, err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.reports.InsertUsageSnapshots(writeCtx, snapshots); err != nil {
		return model.UsageSnapshotCollection{}, reportStorageUnavailable("failed to insert usage snapshots", err)
	}
	return model.UsageSnapshotCollection{
		BucketStart:    bucketStart,
		CollectedAt:    collectedAt,
		SnapshotsCount: len(snapshots),
	}, nil
}

func (s *ReportService) RefreshUsageSnapshots(ctx context.Context) (model.UsageSnapshotCollection, error) {
	return s.CollectUsageSnapshot(ctx, s.now().UTC())
}

func (s *ReportService) collectSnapshots(ctx context.Context, bucketStart, collectedAt time.Time) ([]model.UsageSnapshot, error) {
	snapshots, err := s.source.CollectUsageSnapshots(ctx, bucketStart, collectedAt)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(snapshots))
	for _, snapshot := range snapshots {
		ids = append(ids, snapshot.OwnerID)
	}
	userMap, err := s.users.GetUsers(ctx, ids)
	if err != nil {
		s.logger.Warn("failed to resolve usage snapshot usernames", "error", err)
		return snapshots, nil
	}
	for i := range snapshots {
		if user, ok := userMap[snapshots[i].OwnerID]; ok {
			snapshots[i].OwnerUsername = user.Username
		}
	}
	return snapshots, nil
}

func normalizeFromTo(from, to, now time.Time) (time.Time, time.Time) {
	if to.IsZero() {
		to = now
	}
	if from.IsZero() || !from.Before(to) {
		from = to.Add(-24 * time.Hour)
	}
	return from.UTC(), to.UTC()
}

func reportStorageUnavailable(message string, err error) error {
	return apperrors.Wrap(apperrors.ErrUnavailable, apperrors.ErrUnavailable.Error(), fmt.Errorf("%s: %w", message, err))
}

func (s *ReportService) usageSnapshotInterval() time.Duration {
	seconds := config.DefaultReportsUsageSnapshotIntervalSeconds
	if s.config != nil {
		seconds = s.config.Get().ReportsUsageSnapshotIntervalSeconds
	}
	if seconds < config.MinReportsUsageSnapshotIntervalSeconds {
		seconds = config.MinReportsUsageSnapshotIntervalSeconds
	}
	if seconds > config.MaxReportsUsageSnapshotIntervalSeconds {
		seconds = config.MaxReportsUsageSnapshotIntervalSeconds
	}
	return time.Duration(seconds) * time.Second
}

type ReportsUsageWorker struct {
	reports *ReportService
	logger  *slog.Logger
}

func NewReportsUsageWorker(reports *ReportService, logger *slog.Logger) *ReportsUsageWorker {
	return &ReportsUsageWorker{
		reports: reports,
		logger:  logging.WithComponent(logger, "reports_usage_worker"),
	}
}

func (w *ReportsUsageWorker) Run(ctx context.Context) {
	w.collect(ctx)
	for {
		timer := time.NewTimer(w.reports.usageSnapshotInterval())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			w.collect(ctx)
		}
	}
}

func (w *ReportsUsageWorker) collect(ctx context.Context) {
	if _, err := w.reports.CollectUsageSnapshot(ctx, time.Now().UTC()); err != nil {
		w.logger.Warn("failed to collect reports usage snapshot", "error", err)
	}
}
