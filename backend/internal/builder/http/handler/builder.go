package handler

import (
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/gin-gonic/gin"
)

// TODO: put into a config
const maxUploadSize = 50 << 20

type BuilderService interface {
	InitBuild(ctx context.Context, job model.BuildJob, archive *multipart.FileHeader) (string, error)
	CancelBuild(ctx context.Context, buildID string) error
	GetLogs(ctx context.Context, buildID string) (io.ReadCloser, error)
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
		httpresponse.Respond(c, http.StatusUnauthorized, apperrors.ErrUnauthorized)
		return
	}

	tag := c.PostForm("tag")
	if tag == "" {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	// TODO: handle if empty
	contextDir := c.PostForm("context")
	dockerfile := c.PostForm("dockerfile")

	file, err := c.FormFile("archive")
	if err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	buildArgs := make(map[string]string)
	argsStr := c.PostForm("build_args")
	if argsStr != "" {
		_ = json.Unmarshal([]byte(argsStr), &buildArgs)
	}

	req := dto.BuildImageRequest{
		OwnerID:    ownerID,
		Tag:        tag,
		ContextDir: contextDir,
		Dockerfile: dockerfile,
		File:       file,
		BuildArgs:  buildArgs,
	}
	job := model.BuildJob{
		OwnerID:    req.OwnerID,
		Tag:        req.Tag,
		ContextDir: req.ContextDir,
		Dockerfile: req.Dockerfile,
		BuildArgs:  req.BuildArgs,
	}

	buildID, err := h.service.InitBuild(c.Request.Context(), job, req.File)
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
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
