package grpc

import (
	"context"
	"time"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ReportLogic interface {
	GetReportsOverview(ctx context.Context, from, to time.Time) (model.ReportsOverview, error)
	ListUserUsageReport(ctx context.Context, from, to time.Time, sort string, limit, offset int) ([]model.UserUsageReportItem, int, error)
	GetUserUsageTimeline(ctx context.Context, ownerID uuid.UUID, from, to time.Time) ([]model.UserUsagePoint, error)
	ListAuditEvents(ctx context.Context, filters model.AuditEventFilters) ([]model.AuditEvent, int, error)
	RecordAuditEvent(ctx context.Context, event model.AuditEvent) error
	RefreshUsageSnapshots(ctx context.Context) (model.UsageSnapshotCollection, error)
}

type ReportHandler struct {
	coreapi.UnimplementedReportAPIServer
	logic ReportLogic
}

func RegisterReportAPI(gRPCServer *grpc.Server, logic ReportLogic) {
	coreapi.RegisterReportAPIServer(gRPCServer, &ReportHandler{logic: logic})
}

func (h *ReportHandler) GetReportsOverview(ctx context.Context, req *coreapi.ReportRangeRequest) (*coreapi.ReportsOverviewResponse, error) {
	overview, err := h.logic.GetReportsOverview(ctx, unixTime(req.GetFrom()), unixTime(req.GetTo()))
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	items := make([]*coreapi.ActionCountData, 0, len(overview.TopActions))
	for _, item := range overview.TopActions {
		items = append(items, &coreapi.ActionCountData{Action: item.Action, Count: item.Count})
	}
	resp := &coreapi.ReportsOverviewResponse{
		From:                         overview.From.Unix(),
		To:                           overview.To.Unix(),
		AuditEventsTotal:             overview.AuditEventsTotal,
		FailedActionsTotal:           overview.FailedActionsTotal,
		ActiveUsersTotal:             overview.ActiveUsersTotal,
		ReservedMemoryBytes:          overview.ReservedMemoryBytes,
		TotalDiskBytes:               overview.TotalDiskBytes,
		TopActions:                   items,
		UsageSnapshotIntervalSeconds: overview.UsageSnapshotIntervalSeconds,
	}
	if overview.LastUsageSnapshotAt != nil {
		resp.LastUsageSnapshotAt = overview.LastUsageSnapshotAt.Unix()
	}
	return resp, nil
}

func (h *ReportHandler) ListUserUsageReport(ctx context.Context, req *coreapi.ListUserUsageReportRequest) (*coreapi.PaginatedUserUsageReportResponse, error) {
	limit, offset := pageLimitOffset(req.GetPage(), req.GetLimit())
	items, total, err := h.logic.ListUserUsageReport(ctx, unixTime(req.GetFrom()), unixTime(req.GetTo()), req.GetSort(), limit, offset)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	pbUsers := make([]*coreapi.UserUsageReportData, 0, len(items))
	for _, item := range items {
		pbUsers = append(pbUsers, userUsageReportToProto(item))
	}
	return &coreapi.PaginatedUserUsageReportResponse{Users: pbUsers, TotalCount: int32(total)}, nil
}

func (h *ReportHandler) GetUserUsageTimeline(ctx context.Context, req *coreapi.GetUserUsageTimelineRequest) (*coreapi.UserUsageTimelineResponse, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}
	points, err := h.logic.GetUserUsageTimeline(ctx, ownerID, unixTime(req.GetFrom()), unixTime(req.GetTo()))
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	pbPoints := make([]*coreapi.UserUsagePointData, 0, len(points))
	for _, point := range points {
		pbPoints = append(pbPoints, &coreapi.UserUsagePointData{
			BucketStart:         point.BucketStart.Unix(),
			ReservedMemoryBytes: point.ReservedMemoryBytes,
			TotalDiskBytes:      point.TotalDiskBytes,
			ContainersTotal:     int32(point.ContainersTotal),
			ContainersRunning:   int32(point.ContainersRunning),
			ActionsTotal:        point.ActionsTotal,
		})
	}
	return &coreapi.UserUsageTimelineResponse{Points: pbPoints}, nil
}

func (h *ReportHandler) ListAuditEvents(ctx context.Context, req *coreapi.ListAuditEventsRequest) (*coreapi.PaginatedAuditEventResponse, error) {
	limit, offset := pageLimitOffset(req.GetPage(), req.GetLimit())
	var actorID *uuid.UUID
	if req.GetActorUserId() != "" {
		parsed, err := uuid.Parse(req.GetActorUserId())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid actor_user_id format")
		}
		actorID = &parsed
	}
	events, total, err := h.logic.ListAuditEvents(ctx, model.AuditEventFilters{
		From:         unixTime(req.GetFrom()),
		To:           unixTime(req.GetTo()),
		ActorUserID:  actorID,
		Action:       req.GetAction(),
		Outcome:      req.GetOutcome(),
		ResourceType: req.GetResourceType(),
		Search:       req.GetSearch(),
		Limit:        limit,
		Offset:       offset,
	})
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	pbEvents := make([]*coreapi.AuditEventData, 0, len(events))
	for _, event := range events {
		pbEvents = append(pbEvents, auditEventToProto(event))
	}
	return &coreapi.PaginatedAuditEventResponse{Events: pbEvents, TotalCount: int32(total)}, nil
}

func (h *ReportHandler) RecordAuditEvent(ctx context.Context, req *coreapi.AuditEventData) (*coreapi.Empty, error) {
	event, err := auditEventFromProto(req)
	if err != nil {
		return nil, err
	}
	if err := h.logic.RecordAuditEvent(ctx, event); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	return &coreapi.Empty{}, nil
}

func (h *ReportHandler) RefreshUsageSnapshots(ctx context.Context, _ *coreapi.Empty) (*coreapi.RefreshUsageSnapshotsResponse, error) {
	collection, err := h.logic.RefreshUsageSnapshots(ctx)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	return &coreapi.RefreshUsageSnapshotsResponse{
		BucketStart:    collection.BucketStart.Unix(),
		CollectedAt:    collection.CollectedAt.Unix(),
		SnapshotsCount: int32(collection.SnapshotsCount),
	}, nil
}

func pageLimitOffset(page, limit int32) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return int(limit), int((page - 1) * limit)
}

func unixTime(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(value, 0).UTC()
}

func userUsageReportToProto(item model.UserUsageReportItem) *coreapi.UserUsageReportData {
	return &coreapi.UserUsageReportData{
		OwnerId:             item.OwnerID.String(),
		OwnerUsername:       item.OwnerUsername,
		ReservedMemoryBytes: item.ReservedMemoryBytes,
		TotalDiskBytes:      item.TotalDiskBytes,
		ContainersTotal:     int32(item.ContainersTotal),
		ContainersRunning:   int32(item.ContainersRunning),
		VolumesTotal:        int32(item.VolumesTotal),
		ImagesTotal:         int32(item.ImagesTotal),
		BuildsTotal:         int32(item.BuildsTotal),
		ProjectsTotal:       int32(item.ProjectsTotal),
		ActionsTotal:        item.ActionsTotal,
	}
}

func auditEventToProto(event model.AuditEvent) *coreapi.AuditEventData {
	return &coreapi.AuditEventData{
		Id:            event.ID.String(),
		OccurredAt:    event.OccurredAt.Unix(),
		ActorUserId:   uuidPtrString(event.ActorUserID),
		ActorUsername: event.ActorUsername,
		ActorScope:    event.ActorScope,
		Action:        event.Action,
		Outcome:       event.Outcome,
		ResourceType:  event.ResourceType,
		ResourceId:    event.ResourceID,
		ResourceName:  event.ResourceName,
		OwnerId:       uuidPtrString(event.OwnerID),
		OwnerUsername: event.OwnerUsername,
		RequestId:     event.RequestID,
		ClientIp:      event.ClientIP,
		UserAgent:     event.UserAgent,
		ErrorCode:     event.ErrorCode,
		DetailsJson:   event.DetailsJSON,
	}
}

func auditEventFromProto(req *coreapi.AuditEventData) (model.AuditEvent, error) {
	var event model.AuditEvent
	if req.GetId() != "" {
		id, err := uuid.Parse(req.GetId())
		if err != nil {
			return event, status.Error(codes.InvalidArgument, "invalid id format")
		}
		event.ID = id
	}
	event.OccurredAt = unixTime(req.GetOccurredAt())
	event.ActorUserID = parseUUIDPtr(req.GetActorUserId())
	event.OwnerID = parseUUIDPtr(req.GetOwnerId())
	event.ActorUsername = req.GetActorUsername()
	event.ActorScope = req.GetActorScope()
	event.Action = req.GetAction()
	event.Outcome = req.GetOutcome()
	event.ResourceType = req.GetResourceType()
	event.ResourceID = req.GetResourceId()
	event.ResourceName = req.GetResourceName()
	event.OwnerUsername = req.GetOwnerUsername()
	event.RequestID = req.GetRequestId()
	event.ClientIP = req.GetClientIp()
	event.UserAgent = req.GetUserAgent()
	event.ErrorCode = req.GetErrorCode()
	event.DetailsJSON = req.GetDetailsJson()
	return event, nil
}

func parseUUIDPtr(raw string) *uuid.UUID {
	if raw == "" {
		return nil
	}
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return nil
	}
	return &parsed
}

func uuidPtrString(value *uuid.UUID) string {
	if value == nil {
		return ""
	}
	return value.String()
}
