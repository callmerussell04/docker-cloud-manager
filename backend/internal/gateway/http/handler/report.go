package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

type ReportService interface {
	GetReportsOverview(ctx context.Context, from, to int64) (model.ReportsOverview, error)
	ListUserUsageReport(ctx context.Context, from, to int64, sort string, page, limit int) ([]model.UserUsageReportItem, int, error)
	GetUserUsageTimeline(ctx context.Context, ownerID string, from, to int64) ([]model.UserUsagePoint, error)
	ListAuditEvents(ctx context.Context, filters model.AuditEventFilters) ([]model.AuditEvent, int, error)
	RecordAuditEvent(ctx context.Context, event model.AuditEvent) error
	RefreshUsageSnapshots(ctx context.Context) (model.RefreshUsageSnapshotsResult, error)
}

func (h *CoreHandler) GetReportsOverview(c *gin.Context) {
	from, to, ok := reportRange(c)
	if !ok {
		return
	}
	overview, err := h.service.GetReportsOverview(c.Request.Context(), from, to)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, reportsOverviewToDTO(overview))
}

func (h *CoreHandler) ListUserUsageReport(c *gin.Context) {
	from, to, ok := reportRange(c)
	if !ok {
		return
	}
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	users, total, err := h.service.ListUserUsageReport(c.Request.Context(), from, to, c.DefaultQuery("sort", "disk"), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.UserUsageReportResponse{Users: userUsageReportToDTO(users), TotalCount: total})
}

func (h *CoreHandler) GetUserUsageTimeline(c *gin.Context) {
	ownerID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	from, to, ok := reportRange(c)
	if !ok {
		return
	}
	points, err := h.service.GetUserUsageTimeline(c.Request.Context(), ownerID, from, to)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.UserUsageTimelineResponse{Points: userUsageTimelineToDTO(points)})
}

func (h *CoreHandler) ListAuditEvents(c *gin.Context) {
	from, to, ok := reportRange(c)
	if !ok {
		return
	}
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	actorID := c.Query("actor_user_id")
	if actorID != "" {
		if _, ok := validateUUIDValue(c, "actor_user_id", actorID); !ok {
			return
		}
	}
	events, total, err := h.service.ListAuditEvents(c.Request.Context(), model.AuditEventFilters{
		From:         from,
		To:           to,
		ActorUserID:  actorID,
		Action:       c.Query("action"),
		Outcome:      c.Query("outcome"),
		ResourceType: c.Query("resource_type"),
		Search:       c.Query("search"),
		Page:         page,
		Limit:        limit,
	})
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.AuditEventsResponse{Events: auditEventsToDTO(events), TotalCount: total})
}

func (h *CoreHandler) RefreshUsageSnapshots(c *gin.Context) {
	result, err := h.service.RefreshUsageSnapshots(c.Request.Context())
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.RefreshUsageSnapshotsResponse{
		BucketStart:    result.BucketStart,
		CollectedAt:    result.CollectedAt,
		SnapshotsCount: result.SnapshotsCount,
	})
}

func reportRange(c *gin.Context) (int64, int64, bool) {
	from, ok := optionalUnixQuery(c, "from")
	if !ok {
		return 0, 0, false
	}
	to, ok := optionalUnixQuery(c, "to")
	if !ok {
		return 0, 0, false
	}
	return from, to, true
}

func optionalUnixQuery(c *gin.Context, key string) (int64, bool) {
	raw := c.Query(key)
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		badRequest(c, key+" must be a positive unix timestamp")
		return 0, false
	}
	return value, true
}

func reportsOverviewToDTO(overview model.ReportsOverview) dto.ReportsOverviewResponse {
	topActions := make([]dto.ActionCountResponse, 0, len(overview.TopActions))
	for _, item := range overview.TopActions {
		topActions = append(topActions, dto.ActionCountResponse{Action: item.Action, Count: item.Count})
	}
	return dto.ReportsOverviewResponse{
		From:                         overview.From,
		To:                           overview.To,
		AuditEventsTotal:             overview.AuditEventsTotal,
		FailedActionsTotal:           overview.FailedActionsTotal,
		ActiveUsersTotal:             overview.ActiveUsersTotal,
		ReservedMemoryBytes:          overview.ReservedMemoryBytes,
		TotalDiskBytes:               overview.TotalDiskBytes,
		LastUsageSnapshotAt:          overview.LastUsageSnapshotAt,
		UsageSnapshotIntervalSeconds: overview.UsageSnapshotIntervalSeconds,
		TopActions:                   topActions,
	}
}

func userUsageReportToDTO(items []model.UserUsageReportItem) []dto.UserUsageReportItemResponse {
	result := make([]dto.UserUsageReportItemResponse, 0, len(items))
	for _, item := range items {
		result = append(result, dto.UserUsageReportItemResponse{
			OwnerID:             item.OwnerID,
			OwnerUsername:       item.OwnerUsername,
			ReservedMemoryBytes: item.ReservedMemoryBytes,
			TotalDiskBytes:      item.TotalDiskBytes,
			ContainersTotal:     item.ContainersTotal,
			ContainersRunning:   item.ContainersRunning,
			VolumesTotal:        item.VolumesTotal,
			ImagesTotal:         item.ImagesTotal,
			BuildsTotal:         item.BuildsTotal,
			ProjectsTotal:       item.ProjectsTotal,
			ActionsTotal:        item.ActionsTotal,
		})
	}
	return result
}

func userUsageTimelineToDTO(points []model.UserUsagePoint) []dto.UserUsagePointResponse {
	result := make([]dto.UserUsagePointResponse, 0, len(points))
	for _, point := range points {
		result = append(result, dto.UserUsagePointResponse{
			BucketStart:         point.BucketStart,
			ReservedMemoryBytes: point.ReservedMemoryBytes,
			TotalDiskBytes:      point.TotalDiskBytes,
			ContainersTotal:     point.ContainersTotal,
			ContainersRunning:   point.ContainersRunning,
			ActionsTotal:        point.ActionsTotal,
		})
	}
	return result
}

func auditEventsToDTO(events []model.AuditEvent) []dto.AuditEventResponse {
	result := make([]dto.AuditEventResponse, 0, len(events))
	for _, event := range events {
		result = append(result, dto.AuditEventResponse{
			ID:            event.ID,
			OccurredAt:    event.OccurredAt,
			ActorUserID:   event.ActorUserID,
			ActorUsername: event.ActorUsername,
			ActorScope:    event.ActorScope,
			Action:        event.Action,
			Outcome:       event.Outcome,
			ResourceType:  event.ResourceType,
			ResourceID:    event.ResourceID,
			ResourceName:  event.ResourceName,
			OwnerID:       event.OwnerID,
			OwnerUsername: event.OwnerUsername,
			RequestID:     event.RequestID,
			ClientIP:      event.ClientIP,
			UserAgent:     event.UserAgent,
			ErrorCode:     event.ErrorCode,
			DetailsJSON:   event.DetailsJSON,
		})
	}
	return result
}
