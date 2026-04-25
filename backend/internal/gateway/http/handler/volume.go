package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/gin-gonic/gin"
)

type VolumeService interface {
	CreateVolume(ctx context.Context, ownerID string, createVolumeDTO dto.CreateVolumeDTO) (string, error)
	DeleteVolume(ctx context.Context, ownerID, volumeID string) error
	GetUserVolumes(ctx context.Context, ownerID string) ([]dto.VolumeDTO, error)
	GetAllVolumes(ctx context.Context, page, limit int) (dto.PaginatedVolumes, error)
	AdminDeleteVolume(ctx context.Context, volumeID string) error
}

func (h *CoreHandler) CreateVolume(c *gin.Context) {
	userID := c.GetString("user_id")
	var createVolumeDTO dto.CreateVolumeDTO

	if err := c.ShouldBindJSON(&createVolumeDTO); err != nil {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	volumeID, err := h.service.CreateVolume(c.Request.Context(), userID, createVolumeDTO)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"volume_id": volumeID})
}

func (h *CoreHandler) GetVolumes(c *gin.Context) {
	userID := c.GetString("user_id")

	volumes, err := h.service.GetUserVolumes(c.Request.Context(), userID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	if volumes == nil {
		volumes = make([]dto.VolumeDTO, 0)
	}

	c.JSON(http.StatusOK, gin.H{"volumes": volumes})
}

func (h *CoreHandler) DeleteVolume(c *gin.Context) {
	userID := c.GetString("user_id")
	volumeID := c.Param("id")

	err := h.service.DeleteVolume(c.Request.Context(), userID, volumeID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "volume deleted"})
}

func (h *CoreHandler) GetAllVolumes(c *gin.Context) {
	page, limit := getPaginationParams(c)
	resp, err := h.service.GetAllVolumes(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"volumes":     resp.Volumes,
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) AdminDeleteVolume(c *gin.Context) {
	volumeID := c.Param("id")
	err := h.service.AdminDeleteVolume(c.Request.Context(), volumeID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "volume deleted by admin"})
}
