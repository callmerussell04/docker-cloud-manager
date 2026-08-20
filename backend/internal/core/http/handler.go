package http

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/gin-gonic/gin"
)

type ComposeHandler struct {
	orchestrator ComposeUseCases
	cfg          ConfigProvider
}

type ComposeUseCases interface {
	StartDeployment(ctx context.Context, projectName, archiveName, composeFile string, archive io.Reader) (uuid.UUID, error)
	StartGitDeployment(ctx context.Context, projectName string, source model.GitSource) (uuid.UUID, error)
}

type ConfigProvider interface {
	Get() config.SystemConfig
}

func NewComposeHandler(orchestrator ComposeUseCases, cfg ConfigProvider) *ComposeHandler {
	return &ComposeHandler{orchestrator: orchestrator, cfg: cfg}
}

func (h *ComposeHandler) DeployCompose(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.cfg.Get().ComposeUploadMaxBytes)

	reader, err := c.Request.MultipartReader()
	if err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	var projectName, composeFile string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
			return
		}
		name := part.FormName()
		if name == "archive" {
			if projectName == "" || part.FileName() == "" {
				_ = part.Close()
				httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
				return
			}
			projectID, err := h.orchestrator.StartDeployment(c.Request.Context(), projectName, part.FileName(), composeFile, part)
			_ = part.Close()
			if err != nil {
				httpresponse.Respond(c, httpresponse.Status(err), err)
				return
			}
			c.JSON(http.StatusAccepted, gin.H{
				"project_id": projectID,
				"message":    "compose deployment started",
			})
			return
		}
		value, err := readComposeFormField(part, 1<<20)
		_ = part.Close()
		if err != nil {
			httpresponse.Respond(c, httpresponse.Status(err), err)
			return
		}
		if name == "project_name" {
			projectName = string(value)
		}
		if name == "compose_file" {
			composeFile = string(value)
		}
	}

	httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
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

func readComposeFormField(r io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, apperrors.New(apperrors.ErrBadRequest, "compose form field exceeds size limit")
	}
	return data, nil
}

func pathUUID(c *gin.Context, name string) (uuid.UUID, bool) {
	value := c.Param(name)
	id, err := uuid.Parse(value)
	if err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.New(apperrors.ErrBadRequest, name+" must be a valid UUID"))
		return uuid.Nil, false
	}
	return id, true
}

func SetupRouter(composeHandler *ComposeHandler, buildHandler *BuildHandler, internalToken string, logger *slog.Logger) *gin.Engine {
	r := gin.New()
	r.Use(logging.RequestIDMiddleware())
	r.Use(logging.AccessLogMiddleware(logger))
	r.Use(logging.RecoveryMiddleware(logger))

	internal := r.Group("/api/v1")
	internal.Use(internalauth.Middleware(internalToken))
	internal.POST("/projects/compose", composeHandler.DeployCompose)
	internal.POST("/projects/compose/git", composeHandler.DeployComposeFromGit)
	internal.GET("/images/build/availability", buildHandler.BuildAvailability)
	internal.POST("/images/build", buildHandler.BuildImage)
	internal.POST("/images/build/git", buildHandler.BuildImageFromGit)
	internal.GET("/builds/:id/logs", buildHandler.BuildLogs)
	internal.GET("/admin/builds/:id/logs", buildHandler.BuildLogs)
	return r
}
