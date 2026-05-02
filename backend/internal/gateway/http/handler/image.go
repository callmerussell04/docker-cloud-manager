package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

type ImageService interface {
	DeleteImage(ctx context.Context, imageID string) error
	GetAllImages(ctx context.Context, page, limit int) (model.PaginatedImages, error)
}

func (h *CoreHandler) GetImages(c *gin.Context) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	resp, err := h.service.GetAllImages(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}

	images := resp.Images
	if images == nil {
		images = []model.Image{}
	}

	c.JSON(http.StatusOK, gin.H{
		"images":      imagesToUserDTO(images),
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) DeleteImage(c *gin.Context) {
	imageID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	err := h.service.DeleteImage(c.Request.Context(), imageID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "image deleted"})
}

func (h *CoreHandler) GetAllImages(c *gin.Context) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	resp, err := h.service.GetAllImages(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"images":      imagesToDTO(resp.Images),
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) AdminDeleteImage(c *gin.Context) {
	imageID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	err := h.service.DeleteImage(c.Request.Context(), imageID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "image deleted by admin"})
}
