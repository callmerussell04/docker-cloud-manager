package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/google/uuid"
)

type ReportsRepository interface {
	RecordAuditEvent(ctx context.Context, event model.AuditEvent) error
	InsertUsageSnapshots(ctx context.Context, snapshots []model.UsageSnapshot) error
	GetReportsOverview(ctx context.Context, from, to time.Time) (model.ReportsOverview, error)
	ListUserUsageReport(ctx context.Context, from, to time.Time, sort string, limit, offset int) ([]model.UserUsageReportItem, int, error)
	GetUserUsageTimeline(ctx context.Context, ownerID uuid.UUID, from, to time.Time) ([]model.UserUsagePoint, error)
	ListAuditEvents(ctx context.Context, filters model.AuditEventFilters) ([]model.AuditEvent, int, error)
}

type UsageSnapshotSource interface {
	CollectUsageSnapshots(ctx context.Context, bucketStart, collectedAt time.Time) ([]model.UsageSnapshot, error)
}

type ReportUserDirectory interface {
	GetUsers(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]model.UserInfo, error)
}

type ReportService struct {
	reports ReportsRepository
	source  UsageSnapshotSource
	users   ReportUserDirectory
	logger  *slog.Logger
	now     func() time.Time
	timeout time.Duration
}

func NewReportService(reports ReportsRepository, source UsageSnapshotSource, users ReportUserDirectory, logger *slog.Logger) *ReportService {
	return &ReportService{
		reports: reports,
		source:  source,
		users:   users,
		logger:  logging.WithComponent(logger, "reports"),
		now:     time.Now,
		timeout: 2 * time.Second,
	}
}

func (s *ReportService) GetReportsOverview(ctx context.Context, from, to time.Time) (model.ReportsOverview, error) {
	from, to = normalizeFromTo(from, to, s.now())
	return s.reports.GetReportsOverview(ctx, from, to)
}

func (s *ReportService) ListUserUsageReport(ctx context.Context, from, to time.Time, sort string, limit, offset int) ([]model.UserUsageReportItem, int, error) {
	from, to = normalizeFromTo(from, to, s.now())
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return s.reports.ListUserUsageReport(ctx, from, to, sort, limit, offset)
}

func (s *ReportService) GetUserUsageTimeline(ctx context.Context, ownerID uuid.UUID, from, to time.Time) ([]model.UserUsagePoint, error) {
	from, to = normalizeFromTo(from, to, s.now())
	points, err := s.reports.GetUserUsageTimeline(ctx, ownerID, from, to)
	if err != nil {
		return nil, err
	}

	currentHour := s.now().UTC().Truncate(time.Hour)
	if currentHour.Before(from) || !currentHour.Before(to) || hasPointForHour(points, currentHour) {
		return points, nil
	}
	snapshots, err := s.collectSnapshots(ctx, currentHour, s.now().UTC())
	if err != nil {
		s.logger.Warn("failed to collect live report snapshot", "owner_id", ownerID, "error", err)
		return points, nil
	}
	for _, snapshot := range snapshots {
		if snapshot.OwnerID != ownerID {
			continue
		}
		points = append(points, model.UserUsagePoint{
			BucketStart:         snapshot.BucketStart,
			ReservedMemoryBytes: snapshot.ReservedMemoryBytes,
			TotalDiskBytes:      snapshot.TotalDiskBytes,
			ContainersTotal:     snapshot.ContainersTotal,
			ContainersRunning:   snapshot.ContainersRunning,
		})
		break
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
	return s.reports.ListAuditEvents(ctx, filters)
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

func (s *ReportService) CollectUsageSnapshot(ctx context.Context, at time.Time) error {
	bucketStart := at.UTC().Truncate(time.Hour)
	collectedAt := at.UTC()
	snapshots, err := s.collectSnapshots(ctx, bucketStart, collectedAt)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.reports.InsertUsageSnapshots(writeCtx, snapshots)
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

func hasPointForHour(points []model.UserUsagePoint, hour time.Time) bool {
	for _, point := range points {
		if point.BucketStart.UTC().Equal(hour) {
			return true
		}
	}
	return false
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
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.collect(ctx)
		}
	}
}

func (w *ReportsUsageWorker) collect(ctx context.Context) {
	if err := w.reports.CollectUsageSnapshot(ctx, time.Now().UTC()); err != nil {
		w.logger.Warn("failed to collect reports usage snapshot", "error", err)
	}
}
