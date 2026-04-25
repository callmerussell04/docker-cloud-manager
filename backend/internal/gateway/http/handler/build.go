package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/gin-gonic/gin"
)

type BuildService interface {
	GetUserBuilds(ctx context.Context, ownerID string) ([]dto.BuildDTO, error)
	DeleteBuild(ctx context.Context, ownerID, buildID string) error
	GetAllBuilds(ctx context.Context, page, limit int) (dto.PaginatedBuilds, error)
	AdminDeleteBuild(ctx context.Context, buildID string) error
}

func (h *CoreHandler) GetBuilds(c *gin.Context) {
	userID := c.GetString("user_id")

	builds, err := h.service.GetUserBuilds(c.Request.Context(), userID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	if builds == nil {
		builds = make([]dto.BuildDTO, 0)
	}

	c.JSON(http.StatusOK, gin.H{"builds": builds})
}

func (h *CoreHandler) DeleteBuild(c *gin.Context) {
	userID := c.GetString("user_id")
	buildID := c.Param("id")

	err := h.service.DeleteBuild(c.Request.Context(), userID, buildID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "build record deleted"})
}

func (h *CoreHandler) GetAllBuilds(c *gin.Context) {
	page, limit := getPaginationParams(c)
	resp, err := h.service.GetAllBuilds(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"builds":      resp.Builds,
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) AdminDeleteBuild(c *gin.Context) {
	buildID := c.Param("id")
	err := h.service.AdminDeleteBuild(c.Request.Context(), buildID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "build deleted by admin"})
}
