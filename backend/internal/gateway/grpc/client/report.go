package grpcclient

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
)

func (c *CoreClient) GetReportsOverview(ctx context.Context, from, to int64) (model.ReportsOverview, error) {
	resp, err := c.reportAPI.GetReportsOverview(ctx, &coreapi.ReportRangeRequest{From: from, To: to})
	if err != nil {
		return model.ReportsOverview{}, grpcerrors.FromGRPC(err)
	}
	topActions := make([]model.ActionCount, 0, len(resp.GetTopActions()))
	for _, item := range resp.GetTopActions() {
		topActions = append(topActions, model.ActionCount{Action: item.GetAction(), Count: item.GetCount()})
	}
	return model.ReportsOverview{
		From:                resp.GetFrom(),
		To:                  resp.GetTo(),
		AuditEventsTotal:    resp.GetAuditEventsTotal(),
		FailedActionsTotal:  resp.GetFailedActionsTotal(),
		ActiveUsersTotal:    resp.GetActiveUsersTotal(),
		ReservedMemoryBytes: resp.GetReservedMemoryBytes(),
		TotalDiskBytes:      resp.GetTotalDiskBytes(),
		TopActions:          topActions,
	}, nil
}

func (c *CoreClient) ListUserUsageReport(ctx context.Context, from, to int64, sort string, page, limit int) ([]model.UserUsageReportItem, int, error) {
	resp, err := c.reportAPI.ListUserUsageReport(ctx, &coreapi.ListUserUsageReportRequest{
		From:  from,
		To:    to,
		Sort:  sort,
		Page:  int32(page),
		Limit: int32(limit),
	})
	if err != nil {
		return nil, 0, grpcerrors.FromGRPC(err)
	}
	items := make([]model.UserUsageReportItem, 0, len(resp.GetUsers()))
	for _, user := range resp.GetUsers() {
		items = append(items, model.UserUsageReportItem{
			OwnerID:             user.GetOwnerId(),
			OwnerUsername:       user.GetOwnerUsername(),
			ReservedMemoryBytes: user.GetReservedMemoryBytes(),
			TotalDiskBytes:      user.GetTotalDiskBytes(),
			ContainersTotal:     int(user.GetContainersTotal()),
			ContainersRunning:   int(user.GetContainersRunning()),
			VolumesTotal:        int(user.GetVolumesTotal()),
			ImagesTotal:         int(user.GetImagesTotal()),
			BuildsTotal:         int(user.GetBuildsTotal()),
			ProjectsTotal:       int(user.GetProjectsTotal()),
			ActionsTotal:        user.GetActionsTotal(),
		})
	}
	return items, int(resp.GetTotalCount()), nil
}

func (c *CoreClient) GetUserUsageTimeline(ctx context.Context, ownerID string, from, to int64) ([]model.UserUsagePoint, error) {
	resp, err := c.reportAPI.GetUserUsageTimeline(ctx, &coreapi.GetUserUsageTimelineRequest{OwnerId: ownerID, From: from, To: to})
	if err != nil {
		return nil, grpcerrors.FromGRPC(err)
	}
	points := make([]model.UserUsagePoint, 0, len(resp.GetPoints()))
	for _, point := range resp.GetPoints() {
		points = append(points, model.UserUsagePoint{
			BucketStart:         point.GetBucketStart(),
			ReservedMemoryBytes: point.GetReservedMemoryBytes(),
			TotalDiskBytes:      point.GetTotalDiskBytes(),
			ContainersTotal:     int(point.GetContainersTotal()),
			ContainersRunning:   int(point.GetContainersRunning()),
			ActionsTotal:        point.GetActionsTotal(),
		})
	}
	return points, nil
}

func (c *CoreClient) ListAuditEvents(ctx context.Context, filters model.AuditEventFilters) ([]model.AuditEvent, int, error) {
	resp, err := c.reportAPI.ListAuditEvents(ctx, &coreapi.ListAuditEventsRequest{
		From:         filters.From,
		To:           filters.To,
		ActorUserId:  filters.ActorUserID,
		Action:       filters.Action,
		Outcome:      filters.Outcome,
		ResourceType: filters.ResourceType,
		Search:       filters.Search,
		Page:         int32(filters.Page),
		Limit:        int32(filters.Limit),
	})
	if err != nil {
		return nil, 0, grpcerrors.FromGRPC(err)
	}
	events := make([]model.AuditEvent, 0, len(resp.GetEvents()))
	for _, event := range resp.GetEvents() {
		events = append(events, auditEventFromProto(event))
	}
	return events, int(resp.GetTotalCount()), nil
}

func (c *CoreClient) RecordAuditEvent(ctx context.Context, event model.AuditEvent) error {
	_, err := c.reportAPI.RecordAuditEvent(ctx, auditEventToProto(event))
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func auditEventFromProto(event *coreapi.AuditEventData) model.AuditEvent {
	return model.AuditEvent{
		ID:            event.GetId(),
		OccurredAt:    event.GetOccurredAt(),
		ActorUserID:   event.GetActorUserId(),
		ActorUsername: event.GetActorUsername(),
		ActorScope:    event.GetActorScope(),
		Action:        event.GetAction(),
		Outcome:       event.GetOutcome(),
		ResourceType:  event.GetResourceType(),
		ResourceID:    event.GetResourceId(),
		ResourceName:  event.GetResourceName(),
		OwnerID:       event.GetOwnerId(),
		OwnerUsername: event.GetOwnerUsername(),
		RequestID:     event.GetRequestId(),
		ClientIP:      event.GetClientIp(),
		UserAgent:     event.GetUserAgent(),
		ErrorCode:     event.GetErrorCode(),
		DetailsJSON:   event.GetDetailsJson(),
	}
}

func auditEventToProto(event model.AuditEvent) *coreapi.AuditEventData {
	return &coreapi.AuditEventData{
		Id:            event.ID,
		OccurredAt:    event.OccurredAt,
		ActorUserId:   event.ActorUserID,
		ActorUsername: event.ActorUsername,
		ActorScope:    event.ActorScope,
		Action:        event.Action,
		Outcome:       event.Outcome,
		ResourceType:  event.ResourceType,
		ResourceId:    event.ResourceID,
		ResourceName:  event.ResourceName,
		OwnerId:       event.OwnerID,
		OwnerUsername: event.OwnerUsername,
		RequestId:     event.RequestID,
		ClientIp:      event.ClientIP,
		UserAgent:     event.UserAgent,
		ErrorCode:     event.ErrorCode,
		DetailsJson:   event.DetailsJSON,
	}
}
