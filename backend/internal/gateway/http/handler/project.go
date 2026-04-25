package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/gin-gonic/gin"
)

type ProjectService interface {
	GetUserProjects(ctx context.Context, ownerID string) ([]dto.ProjectDTO, error)
	DeleteProject(ctx context.Context, ownerID, projectID string) error
	StopProject(ctx context.Context, ownerID, projectID string) error
	GetAllProjects(ctx context.Context, page, limit int) (dto.PaginatedProjects, error)
	AdminDeleteProject(ctx context.Context, projectID string) error
	AdminStopProject(ctx context.Context, projectID string) error
}

func (h *CoreHandler) GetProjects(c *gin.Context) {
	userID := c.GetString("user_id")

	projects, err := h.service.GetUserProjects(c.Request.Context(), userID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	if projects == nil {
		projects = make([]dto.ProjectDTO, 0)
	}

	c.JSON(http.StatusOK, gin.H{"projects": projects})
}

func (h *CoreHandler) DeleteProject(c *gin.Context) {
	userID := c.GetString("user_id")
	projectID := c.Param("id")

	err := h.service.DeleteProject(c.Request.Context(), userID, projectID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "project deleted"})
}

func (h *CoreHandler) StopProject(c *gin.Context) {
	userID := c.GetString("user_id")
	projectID := c.Param("id")

	err := h.service.StopProject(c.Request.Context(), userID, projectID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "project stopped successfully"})
}

func (h *CoreHandler) GetAllProjects(c *gin.Context) {
	page, limit := getPaginationParams(c)
	resp, err := h.service.GetAllProjects(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"projects":    resp.Projects,
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) AdminDeleteProject(c *gin.Context) {
	projectID := c.Param("id")
	err := h.service.AdminDeleteProject(c.Request.Context(), projectID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "project deleted by admin"})
}

func (h *CoreHandler) AdminStopProject(c *gin.Context) {
	projectID := c.Param("id")
	err := h.service.AdminStopProject(c.Request.Context(), projectID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "project stopped by admin"})
}
