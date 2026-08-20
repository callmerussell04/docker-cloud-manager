package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/auditlog"
	"github.com/gin-gonic/gin"
)

type ProjectService interface {
	DeleteProject(ctx context.Context, projectID string) error
	StartProject(ctx context.Context, projectID string) error
	StopProject(ctx context.Context, projectID string) error
	CancelProject(ctx context.Context, projectID string) error
	ListProjects(ctx context.Context, page, limit int) (model.PaginatedProjects, error)
}

func (h *CoreHandler) GetProjects(c *gin.Context) {
	h.listProjects(c, func(items []model.Project) any {
		return projectsToUserDTO(items)
	})
}

func (h *CoreHandler) ListAdminProjects(c *gin.Context) {
	h.listProjects(c, func(items []model.Project) any {
		return projectsToDTO(items)
	})
}

func (h *CoreHandler) listProjects(c *gin.Context, mapItems func([]model.Project) any) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	resp, err := h.service.ListProjects(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"projects":    mapItems(resp.Projects),
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) DeleteProject(c *gin.Context) {
	h.deleteProject(c, "project delete requested")
}

func (h *CoreHandler) deleteProject(c *gin.Context, message string) {
	projectID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	err := h.service.DeleteProject(c.Request.Context(), projectID)
	outcome, errorCode := auditOutcome(err)
	h.recordAudit(c, auditInput{
		Action:       auditlog.ActionProjectDelete,
		ResourceType: auditlog.ResourceProject,
		ResourceID:   projectID,
		Outcome:      outcome,
		ErrorCode:    errorCode,
	})
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": message})
}

func (h *CoreHandler) StartProject(c *gin.Context) {
	h.runProjectAction(c, h.service.StartProject, auditlog.ActionProjectStart, "project start requested")
}

func (h *CoreHandler) StopProject(c *gin.Context) {
	h.runProjectAction(c, h.service.StopProject, auditlog.ActionProjectStop, "project stop requested")
}

func (h *CoreHandler) CancelProject(c *gin.Context) {
	h.runProjectAction(c, h.service.CancelProject, auditlog.ActionProjectCancel, "project cancellation requested")
}

func (h *CoreHandler) runProjectAction(c *gin.Context, action func(context.Context, string) error, auditAction, message string) {
	projectID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	err := action(c.Request.Context(), projectID)
	outcome, errorCode := auditOutcome(err)
	h.recordAudit(c, auditInput{
		Action:       auditAction,
		ResourceType: auditlog.ResourceProject,
		ResourceID:   projectID,
		Outcome:      outcome,
		ErrorCode:    errorCode,
	})
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": message})
}

func (h *CoreHandler) AdminDeleteProject(c *gin.Context) {
	h.deleteProject(c, "project delete requested by admin")
}

func (h *CoreHandler) AdminStartProject(c *gin.Context) {
	h.runProjectAction(c, h.service.StartProject, auditlog.ActionProjectStart, "project start requested by admin")
}

func (h *CoreHandler) AdminStopProject(c *gin.Context) {
	h.runProjectAction(c, h.service.StopProject, auditlog.ActionProjectStop, "project stop requested by admin")
}

func (h *CoreHandler) AdminCancelProject(c *gin.Context) {
	h.runProjectAction(c, h.service.CancelProject, auditlog.ActionProjectCancel, "project cancellation requested by admin")
}
