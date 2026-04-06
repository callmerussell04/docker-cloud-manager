package http

import (
	"io"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ComposeHandler struct {
	orchestrator *compose.Orchestrator
}

func NewComposeHandler(orchestrator *compose.Orchestrator) *ComposeHandler {
	return &ComposeHandler{orchestrator: orchestrator}
}

func (h *ComposeHandler) DeployCompose(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 100<<20)

	ownerIDStr := c.GetHeader("X-User-Id")
	ownerID, err := uuid.Parse(ownerIDStr)
	if err != nil {
		apperrors.Respond(c, http.StatusUnauthorized, apperrors.ErrUnauthorized)
		return
	}

	projectName := c.PostForm("project_name")
	if projectName == "" {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	file, err := c.FormFile("archive")
	if err != nil {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	src, err := file.Open()
	if err != nil {
		apperrors.Respond(c, http.StatusInternalServerError, apperrors.ErrInternal)
		return
	}
	defer src.Close()

	archiveBytes, err := io.ReadAll(src)
	if err != nil {
		apperrors.Respond(c, http.StatusInternalServerError, apperrors.ErrInternal)
		return
	}

	projectID, err := h.orchestrator.StartDeployment(c.Request.Context(), ownerID, projectName, archiveBytes)
	if err != nil {
		apperrors.Respond(c, http.StatusInternalServerError, apperrors.ErrInternal)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"project_id": projectID,
		"message":    "compose deployment started",
	})
}

func SetupRouter(handler *ComposeHandler) *gin.Engine {
	r := gin.Default()
	r.POST("/api/v1/internal/compose", handler.DeployCompose)
	return r
}
