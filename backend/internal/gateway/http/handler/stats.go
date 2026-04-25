package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/gin-gonic/gin"
)

type StatsService interface {
	GetUserStats(ctx context.Context, ownerID string) (dto.UserStatsDTO, error)
}

func (h *CoreHandler) GetUserStats(c *gin.Context) {
	userID := c.GetString("user_id")

	stats, err := h.service.GetUserStats(c.Request.Context(), userID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, stats)
}
