package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
)

type ReportProvider interface {
	GetReportsOverview(ctx context.Context, from, to int64) (model.ReportsOverview, error)
	ListUserUsageReport(ctx context.Context, from, to int64, sort string, page, limit int) ([]model.UserUsageReportItem, int, error)
	GetUserUsageTimeline(ctx context.Context, ownerID string, from, to int64) ([]model.UserUsagePoint, error)
	ListAuditEvents(ctx context.Context, filters model.AuditEventFilters) ([]model.AuditEvent, int, error)
	RecordAuditEvent(ctx context.Context, event model.AuditEvent) error
	RefreshUsageSnapshots(ctx context.Context) (model.RefreshUsageSnapshotsResult, error)
}

func (s *Core) GetReportsOverview(ctx context.Context, from, to int64) (model.ReportsOverview, error) {
	return s.provider.GetReportsOverview(ctx, from, to)
}

func (s *Core) ListUserUsageReport(ctx context.Context, from, to int64, sort string, page, limit int) ([]model.UserUsageReportItem, int, error) {
	return s.provider.ListUserUsageReport(ctx, from, to, sort, page, limit)
}

func (s *Core) GetUserUsageTimeline(ctx context.Context, ownerID string, from, to int64) ([]model.UserUsagePoint, error) {
	return s.provider.GetUserUsageTimeline(ctx, ownerID, from, to)
}

func (s *Core) ListAuditEvents(ctx context.Context, filters model.AuditEventFilters) ([]model.AuditEvent, int, error) {
	return s.provider.ListAuditEvents(ctx, filters)
}

func (s *Core) RecordAuditEvent(ctx context.Context, event model.AuditEvent) error {
	return s.provider.RecordAuditEvent(ctx, event)
}

func (s *Core) RefreshUsageSnapshots(ctx context.Context) (model.RefreshUsageSnapshotsResult, error) {
	return s.provider.RefreshUsageSnapshots(ctx)
}
