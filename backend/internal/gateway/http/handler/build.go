package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/auditlog"
	"github.com/gin-gonic/gin"
)

type BuildService interface {
	GetBuild(ctx context.Context, buildID string) (model.Build, error)
	CancelBuildRecord(ctx context.Context, buildID string) error
	DeleteBuild(ctx context.Context, buildID string) error
	ListBuilds(ctx context.Context, page, limit int) (model.PaginatedBuilds, error)
}

func (h *CoreHandler) GetBuilds(c *gin.Context) {
	h.listBuilds(c, func(items []model.Build) any {
		return buildsToUserDTO(items)
	})
}

func (h *CoreHandler) ListAdminBuilds(c *gin.Context) {
	h.listBuilds(c, func(items []model.Build) any {
		return buildsToDTO(items)
	})
}

func (h *CoreHandler) listBuilds(c *gin.Context, mapItems func([]model.Build) any) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	resp, err := h.service.ListBuilds(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"builds":      mapItems(resp.Builds),
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) CancelBuild(c *gin.Context) {
	h.cancelBuild(c, "build cancellation requested")
}

func (h *CoreHandler) cancelBuild(c *gin.Context, message string) {
	buildID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	err := h.service.CancelBuildRecord(c.Request.Context(), buildID)
	outcome, errorCode := auditOutcome(err)
	h.recordAudit(c, auditInput{
		Action:       auditlog.ActionBuildCancel,
		ResourceType: auditlog.ResourceBuild,
		ResourceID:   buildID,
		Outcome:      outcome,
		ErrorCode:    errorCode,
	})
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": message})
}

func (h *CoreHandler) AdminCancelBuild(c *gin.Context) {
	h.cancelBuild(c, "build cancellation requested")
}

func (h *CoreHandler) DeleteBuild(c *gin.Context) {
	h.deleteBuild(c, "build record deleted")
}

func (h *CoreHandler) deleteBuild(c *gin.Context, message string) {
	buildID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	err := h.service.DeleteBuild(c.Request.Context(), buildID)
	outcome, errorCode := auditOutcome(err)
	h.recordAudit(c, auditInput{
		Action:       auditlog.ActionBuildDelete,
		ResourceType: auditlog.ResourceBuild,
		ResourceID:   buildID,
		Outcome:      outcome,
		ErrorCode:    errorCode,
	})
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": message})
}

func (h *CoreHandler) AdminDeleteBuild(c *gin.Context) {
	h.deleteBuild(c, "build deleted by admin")
}
