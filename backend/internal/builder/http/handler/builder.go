package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/domain"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/gin-gonic/gin"
)

// TODO: put into a config
const maxUploadSize = 50 << 20

type BuilderService interface {
	InitBuild(ctx context.Context, job domain.BuildJob) (string, error)
}

type BuildHandler struct {
	service BuilderService
	logsDir string
}

func NewBuildHandler(service BuilderService, logsDir string) *BuildHandler {
	return &BuildHandler{
		service: service,
		logsDir: logsDir,
	}
}

func (h *BuildHandler) BuildImage(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadSize)

	ownerID := c.GetHeader("X-User-Id")
	if ownerID == "" {
		apperrors.Respond(c, http.StatusUnauthorized, apperrors.ErrUnauthorized)
		return
	}

	tag := c.PostForm("tag")
	if tag == "" {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	// TODO: handle if empty
	contextDir := c.PostForm("context")
	dockerfile := c.PostForm("dockerfile")

	file, err := c.FormFile("archive")
	if err != nil {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	buildArgs := make(map[string]string)
	argsStr := c.PostForm("build_args")
	if argsStr != "" {
		_ = json.Unmarshal([]byte(argsStr), &buildArgs)
	}

	job := domain.BuildJob{
		OwnerID:    ownerID,
		Tag:        tag,
		ContextDir: contextDir,
		Dockerfile: dockerfile,
		File:       file,
		BuildArgs:  buildArgs,
	}

	buildID, err := h.service.InitBuild(c.Request.Context(), job)
	if err != nil {
		if err == apperrors.ErrInvalidFileFormat {
			apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrInvalidFileFormat)
			return
		}
		apperrors.Respond(c, http.StatusInternalServerError, apperrors.ErrInternal)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"build_id": buildID,
		"message":  "build initialized",
	})
}

func (h *BuildHandler) GetLogs(c *gin.Context) {
	buildID := c.Param("id")
	if buildID == "" {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	logPath := filepath.Join(h.logsDir, buildID+".log")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		apperrors.Respond(c, http.StatusNotFound, apperrors.ErrNotFound)
		return
	}

	c.File(logPath)
}
