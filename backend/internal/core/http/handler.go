package http

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
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

	req := dto.DeployComposeRequest{
		OwnerID:     ownerIDStr,
		ProjectName: projectName,
		Archive:     file,
	}

	ownerID, err := uuid.Parse(req.OwnerID)
	if err != nil {
		apperrors.Respond(c, http.StatusUnauthorized, apperrors.ErrUnauthorized)
		return
	}

	src, err := req.Archive.Open()
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

	projectID, err := h.orchestrator.StartDeployment(c.Request.Context(), ownerID, req.ProjectName, archiveBytes)
	if err != nil {
		apperrors.Respond(c, apperrors.HTTPStatus(err), err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"project_id": projectID,
		"message":    "compose deployment started",
	})
}

func SetupRouter(handler *ComposeHandler, internalToken string, logger *slog.Logger) *gin.Engine {
	r := gin.New()
	r.Use(logging.RequestIDMiddleware())
	r.Use(logging.AccessLogMiddleware(logger))
	r.Use(logging.RecoveryMiddleware(logger))
	r.POST("/api/v1/projects/compose", internalauth.Middleware(internalToken), handler.DeployCompose)
	return r
}
