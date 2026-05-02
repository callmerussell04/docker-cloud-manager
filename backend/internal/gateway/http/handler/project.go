package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

type ProjectService interface {
	DeleteProject(ctx context.Context, projectID string) error
	StartProject(ctx context.Context, projectID string) error
	StopProject(ctx context.Context, projectID string) error
	GetAllProjects(ctx context.Context, page, limit int) (model.PaginatedProjects, error)
}

func (h *CoreHandler) GetProjects(c *gin.Context) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	resp, err := h.service.GetAllProjects(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}

	projects := resp.Projects
	if projects == nil {
		projects = []model.Project{}
	}

	c.JSON(http.StatusOK, gin.H{
		"projects":    projectsToUserDTO(projects),
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) DeleteProject(c *gin.Context) {
	projectID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	err := h.service.DeleteProject(c.Request.Context(), projectID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "project deleted"})
}

func (h *CoreHandler) StartProject(c *gin.Context) {
	projectID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	err := h.service.StartProject(c.Request.Context(), projectID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "project started successfully"})
}

func (h *CoreHandler) StopProject(c *gin.Context) {
	projectID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	err := h.service.StopProject(c.Request.Context(), projectID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "project stopped successfully"})
}

func (h *CoreHandler) GetAllProjects(c *gin.Context) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	resp, err := h.service.GetAllProjects(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"projects":    projectsToDTO(resp.Projects),
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) AdminDeleteProject(c *gin.Context) {
	projectID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	err := h.service.DeleteProject(c.Request.Context(), projectID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "project deleted by admin"})
}

func (h *CoreHandler) AdminStartProject(c *gin.Context) {
	projectID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	err := h.service.StartProject(c.Request.Context(), projectID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "project started by admin"})
}

func (h *CoreHandler) AdminStopProject(c *gin.Context) {
	projectID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	err := h.service.StopProject(c.Request.Context(), projectID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "project stopped by admin"})
}
