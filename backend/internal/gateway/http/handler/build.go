package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

type BuildService interface {
	GetBuild(ctx context.Context, buildID string) (model.Build, error)
	DeleteBuild(ctx context.Context, buildID string) error
	GetAllBuilds(ctx context.Context, page, limit int) (model.PaginatedBuilds, error)
}

func (h *CoreHandler) GetBuilds(c *gin.Context) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	resp, err := h.service.GetAllBuilds(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}

	builds := resp.Builds
	if builds == nil {
		builds = []model.Build{}
	}

	c.JSON(http.StatusOK, gin.H{
		"builds":      buildsToUserDTO(builds),
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) DeleteBuild(c *gin.Context) {
	buildID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	err := h.service.DeleteBuild(c.Request.Context(), buildID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "build record deleted"})
}

func (h *CoreHandler) GetAllBuilds(c *gin.Context) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	resp, err := h.service.GetAllBuilds(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"builds":      buildsToDTO(resp.Builds),
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) AdminDeleteBuild(c *gin.Context) {
	buildID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	err := h.service.DeleteBuild(c.Request.Context(), buildID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "build deleted by admin"})
}
