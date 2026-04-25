package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/gin-gonic/gin"
)

type SystemService interface {
	GetSystemConfig(ctx context.Context) (dto.SystemConfigDTO, error)
	UpdateSystemConfig(ctx context.Context, req dto.SystemConfigDTO) error
}

func (h *CoreHandler) GetSystemConfig(c *gin.Context) {
	resp, err := h.service.GetSystemConfig(c.Request.Context())
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *CoreHandler) UpdateSystemConfig(c *gin.Context) {
	var systemConfigDTO dto.SystemConfigDTO
	if err := c.ShouldBindJSON(&systemConfigDTO); err != nil {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	err := h.service.UpdateSystemConfig(c.Request.Context(), systemConfigDTO)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "config updated"})
}
