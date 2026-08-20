package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

type ReadinessService interface {
	Ready(ctx context.Context) map[string]error
}

type HealthHandler struct {
	readiness ReadinessService
}

func NewHealthHandler(readiness ReadinessService) *HealthHandler {
	return &HealthHandler{readiness: readiness}
}

func (h *HealthHandler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *HealthHandler) Ready(c *gin.Context) {
	checks := h.readiness.Ready(c.Request.Context())
	resp := gin.H{
		"status": "ok",
		"checks": gin.H{},
	}
	statusCode := http.StatusOK
	checkResp := resp["checks"].(gin.H)
	for name, err := range checks {
		if err != nil {
			statusCode = http.StatusServiceUnavailable
			resp["status"] = "degraded"
			checkResp[name] = gin.H{
				"status": "unavailable",
				"error":  err.Error(),
			}
			continue
		}
		checkResp[name] = gin.H{"status": "ok"}
	}
	c.JSON(statusCode, resp)
}
