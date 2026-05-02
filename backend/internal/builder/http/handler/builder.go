package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/gin-gonic/gin"
)

type BuilderService interface {
	InitBuild(ctx context.Context, job model.BuildJob, archiveName string, archive io.Reader) (string, error)
	CancelBuild(ctx context.Context, buildID string) error
	GetLogs(ctx context.Context, buildID string) (io.ReadCloser, error)
}

type BuildHandler struct {
	service BuilderService
	logsDir string
	config  *config.RuntimeManager
}

func NewBuildHandler(service BuilderService, logsDir string, config *config.RuntimeManager) *BuildHandler {
	return &BuildHandler{
		service: service,
		logsDir: logsDir,
		config:  config,
	}
}

func (h *BuildHandler) BuildImage(c *gin.Context) {
	cfg := h.config.Refresh(c.Request.Context())
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, cfg.MaxUploadSizeBytes)

	if _, err := accessscope.RequireUserOwner(c.Request.Context()); err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}

	reader, err := c.Request.MultipartReader()
	if err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	var tag, contextDir, dockerfile string
	buildArgs := make(map[string]string)
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
			if tag == "" {
				httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
				_ = part.Close()
				return
			}
			req := dto.BuildImageRequest{
				Tag:        tag,
				ContextDir: contextDir,
				Dockerfile: dockerfile,
				BuildArgs:  buildArgs,
			}
			job := model.BuildJob{
				Tag:        req.Tag,
				ContextDir: req.ContextDir,
				Dockerfile: req.Dockerfile,
				BuildArgs:  req.BuildArgs,
			}
			buildID, err := h.service.InitBuild(c.Request.Context(), job, part.FileName(), part)
			_ = part.Close()
			if err != nil {
				httpresponse.Respond(c, httpresponse.Status(err), err)
				return
			}
			c.JSON(http.StatusAccepted, gin.H{
				"build_id": buildID,
				"message":  "build initialized",
			})
			return
		}
		value, err := io.ReadAll(io.LimitReader(part, 1<<20))
		_ = part.Close()
		if err != nil {
			httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
			return
		}
		switch name {
		case "tag":
			tag = string(value)
		case "context":
			contextDir = string(value)
		case "dockerfile":
			dockerfile = string(value)
		case "build_args":
			if len(value) > 0 {
				_ = json.Unmarshal(value, &buildArgs)
			}
		}
	}

	httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
}

func (h *BuildHandler) GetLogs(c *gin.Context) {
	buildID := c.Param("id")
	if buildID == "" {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	logReader, err := h.service.GetLogs(c.Request.Context(), buildID)
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}
	defer logReader.Close()

	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, logReader)
}

func (h *BuildHandler) CancelBuild(c *gin.Context) {
	buildID := c.Param("id")
	if buildID == "" {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	if err := h.service.CancelBuild(c.Request.Context(), buildID); err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "build cancellation requested"})
}
