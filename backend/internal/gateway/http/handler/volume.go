package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
	"github.com/gin-gonic/gin"
)

type VolumeService interface {
	CreateVolume(ctx context.Context, input model.CreateVolumeInput) (string, error)
	DeleteVolume(ctx context.Context, volumeID string) error
	GetAllVolumes(ctx context.Context, page, limit int) (model.PaginatedVolumes, error)
}

func (h *CoreHandler) CreateVolume(c *gin.Context) {
	var createVolumeDTO dto.CreateVolumeDTO

	if err := c.ShouldBindJSON(&createVolumeDTO); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}
	if err := validation.ResourceName(createVolumeDTO.Name); err != nil {
		badRequest(c, err.Error())
		return
	}

	volumeID, err := h.service.CreateVolume(c.Request.Context(), createVolumeInputFromDTO(createVolumeDTO))
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"volume_id": volumeID})
}

func (h *CoreHandler) GetVolumes(c *gin.Context) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	resp, err := h.service.GetAllVolumes(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}

	volumes := resp.Volumes
	if volumes == nil {
		volumes = []model.Volume{}
	}

	c.JSON(http.StatusOK, gin.H{
		"volumes":     volumesToUserDTO(volumes),
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) DeleteVolume(c *gin.Context) {
	volumeID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	err := h.service.DeleteVolume(c.Request.Context(), volumeID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "volume deleted"})
}

func (h *CoreHandler) GetAllVolumes(c *gin.Context) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	resp, err := h.service.GetAllVolumes(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"volumes":     volumesToDTO(resp.Volumes),
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) AdminDeleteVolume(c *gin.Context) {
	volumeID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	err := h.service.DeleteVolume(c.Request.Context(), volumeID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "volume deleted by admin"})
}
