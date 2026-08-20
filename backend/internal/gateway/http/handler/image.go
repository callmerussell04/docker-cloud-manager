package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/auditlog"
	"github.com/gin-gonic/gin"
)

type ImageService interface {
	DeleteImage(ctx context.Context, imageID string) error
	ListImages(ctx context.Context, page, limit int) (model.PaginatedImages, error)
}

func (h *CoreHandler) GetImages(c *gin.Context) {
	h.listImages(c, func(items []model.Image) any {
		return imagesToUserDTO(items)
	})
}

func (h *CoreHandler) ListAdminImages(c *gin.Context) {
	h.listImages(c, func(items []model.Image) any {
		return imagesToDTO(items)
	})
}

func (h *CoreHandler) listImages(c *gin.Context, mapItems func([]model.Image) any) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	resp, err := h.service.ListImages(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"images":      mapItems(resp.Images),
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) DeleteImage(c *gin.Context) {
	h.deleteImage(c, "image deleted")
}

func (h *CoreHandler) deleteImage(c *gin.Context, message string) {
	imageID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	err := h.service.DeleteImage(c.Request.Context(), imageID)
	outcome, errorCode := auditOutcome(err)
	h.recordAudit(c, auditInput{
		Action:       auditlog.ActionImageDelete,
		ResourceType: auditlog.ResourceImage,
		ResourceID:   imageID,
		Outcome:      outcome,
		ErrorCode:    errorCode,
	})
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": message})
}

func (h *CoreHandler) AdminDeleteImage(c *gin.Context) {
	h.deleteImage(c, "image deleted by admin")
}
