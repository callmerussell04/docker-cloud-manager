package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/gin-gonic/gin"
)

type SystemService interface {
	GetSystemConfig(ctx context.Context) (model.SystemConfig, error)
	UpdateSystemConfig(ctx context.Context, req model.SystemConfig) error
}

func (h *CoreHandler) GetSystemConfig(c *gin.Context) {
	resp, err := h.service.GetSystemConfig(c.Request.Context())
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, systemConfigToDTO(resp))
}

func (h *CoreHandler) UpdateSystemConfig(c *gin.Context) {
	var systemConfigDTO dto.SystemConfigDTO
	if err := c.ShouldBindJSON(&systemConfigDTO); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	err := h.service.UpdateSystemConfig(c.Request.Context(), systemConfigFromDTO(systemConfigDTO))
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "config updated"})
}
