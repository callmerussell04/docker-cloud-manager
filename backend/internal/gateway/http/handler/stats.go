package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

type StatsService interface {
	GetUserStats(ctx context.Context) (model.UserStats, error)
	GetSystemMonitoring(ctx context.Context) (model.SystemMonitoring, error)
}

func (h *CoreHandler) GetUserStats(c *gin.Context) {
	stats, err := h.service.GetUserStats(c.Request.Context())
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, userStatsToDTO(stats))
}

func (h *CoreHandler) GetSystemMonitoring(c *gin.Context) {
	stats, err := h.service.GetSystemMonitoring(c.Request.Context())
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, systemMonitoringToDTO(stats))
}
