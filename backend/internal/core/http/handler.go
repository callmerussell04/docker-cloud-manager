package http

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/gin-gonic/gin"
)

type ComposeHandler struct {
	orchestrator *compose.Orchestrator
	cfg          ConfigProvider
}

type ConfigProvider interface {
	Get() config.SystemConfig
}

func NewComposeHandler(orchestrator *compose.Orchestrator, cfg ConfigProvider) *ComposeHandler {
	return &ComposeHandler{orchestrator: orchestrator, cfg: cfg}
}

func (h *ComposeHandler) DeployCompose(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.cfg.Get().ComposeUploadMaxBytes)

	projectName := c.PostForm("project_name")
	if projectName == "" {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	file, err := c.FormFile("archive")
	if err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	req := dto.DeployComposeRequest{
		ProjectName: projectName,
		Archive:     file,
	}

	src, err := req.Archive.Open()
	if err != nil {
		httpresponse.Respond(c, http.StatusInternalServerError, apperrors.ErrInternal)
		return
	}
	defer src.Close()

	archiveBytes, err := io.ReadAll(src)
	if err != nil {
		httpresponse.Respond(c, http.StatusInternalServerError, apperrors.ErrInternal)
		return
	}

	projectID, err := h.orchestrator.StartDeployment(c.Request.Context(), req.ProjectName, archiveBytes)
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"project_id": projectID,
		"message":    "compose deployment started",
	})
}

func (h *ComposeHandler) DeployComposeFromGit(c *gin.Context) {
	var req dto.DeployComposeGitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}
	if req.ProjectName == "" || req.RepoURL == "" {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	projectID, err := h.orchestrator.StartGitDeployment(c.Request.Context(), req.ProjectName, model.GitSource{
		RepoURL:     req.RepoURL,
		Ref:         req.Ref,
		ComposeFile: req.ComposeFile,
	})
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
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
	r.POST("/api/v1/projects/compose/git", internalauth.Middleware(internalToken), handler.DeployComposeFromGit)
	return r
}
