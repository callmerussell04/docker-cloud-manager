package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

type ImageService interface {
	GetUserImages(ctx context.Context, ownerID string) ([]model.Image, error)
	DeleteImage(ctx context.Context, ownerID, imageID string) error
	GetAllImages(ctx context.Context, page, limit int) (model.PaginatedImages, error)
	AdminDeleteImage(ctx context.Context, imageID string) error
}

func (h *CoreHandler) GetImages(c *gin.Context) {
	userID := c.GetString("user_id")

	images, err := h.service.GetUserImages(c.Request.Context(), userID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	if images == nil {
		images = make([]model.Image, 0)
	}

	c.JSON(http.StatusOK, gin.H{"images": imagesToUserDTO(images)})
}

func (h *CoreHandler) DeleteImage(c *gin.Context) {
	userID := c.GetString("user_id")
	imageID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	err := h.service.DeleteImage(c.Request.Context(), userID, imageID)
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
	err := h.service.AdminDeleteImage(c.Request.Context(), imageID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "image deleted by admin"})
}
