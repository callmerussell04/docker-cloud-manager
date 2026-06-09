package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
	"github.com/gin-gonic/gin"
)

type VolumeService interface {
	CreateVolume(ctx context.Context, input model.CreateVolumeInput) (string, error)
	DeleteVolume(ctx context.Context, volumeID string) error
	ListVolumes(ctx context.Context, page, limit int) (model.PaginatedVolumes, error)
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
	outcome, errorCode := auditOutcome(err)
	h.recordAudit(c, auditInput{
		Action:       auditlog.ActionVolumeCreate,
		ResourceType: auditlog.ResourceVolume,
		ResourceID:   volumeID,
		ResourceName: createVolumeDTO.Name,
		Outcome:      outcome,
		ErrorCode:    errorCode,
	})
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"volume_id": volumeID})
}

func (h *CoreHandler) GetVolumes(c *gin.Context) {
	h.listVolumes(c, func(items []model.Volume) any {
		return volumesToUserDTO(items)
	})
}

func (h *CoreHandler) ListAdminVolumes(c *gin.Context) {
	h.listVolumes(c, func(items []model.Volume) any {
		return volumesToDTO(items)
	})
}

func (h *CoreHandler) listVolumes(c *gin.Context, mapItems func([]model.Volume) any) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	resp, err := h.service.ListVolumes(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"volumes":     mapItems(resp.Volumes),
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) DeleteVolume(c *gin.Context) {
	h.deleteVolume(c, "volume deleted")
}

func (h *CoreHandler) deleteVolume(c *gin.Context, message string) {
	volumeID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	err := h.service.DeleteVolume(c.Request.Context(), volumeID)
	outcome, errorCode := auditOutcome(err)
	h.recordAudit(c, auditInput{
		Action:       auditlog.ActionVolumeDelete,
		ResourceType: auditlog.ResourceVolume,
		ResourceID:   volumeID,
		Outcome:      outcome,
		ErrorCode:    errorCode,
	})
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": message})
}

func (h *CoreHandler) AdminDeleteVolume(c *gin.Context) {
	h.deleteVolume(c, "volume deleted by admin")
}
