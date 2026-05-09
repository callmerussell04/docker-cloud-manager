package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
)

type BuildUseCases interface {
	CreateBuildFromArchive(ctx context.Context, input service.BuildArchiveInput) (service.BuildInitResult, error)
	CreateBuildFromGit(ctx context.Context, input service.BuildGitInput) (service.BuildInitResult, error)
	OpenBuildLogs(ctx context.Context, buildID uuid.UUID) (io.ReadCloser, error)
}

type BuildHandler struct {
	builds BuildUseCases
	cfg    ConfigProvider
}

const maxBuildFormFieldBytes = 1 << 20
const imageBuildsUnavailableMessage = "Image builds are currently unavailable. Use Docker Hub images."
const gitSourcesUnavailableMessage = "Git sources are currently unavailable. Upload an archive instead."

func NewBuildHandler(builds BuildUseCases, cfg ConfigProvider) *BuildHandler {
	return &BuildHandler{builds: builds, cfg: cfg}
}

func (h *BuildHandler) BuildImage(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.cfg.Get().MaxUploadSizeBytes)

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
			if tag == "" || part.FileName() == "" {
				_ = part.Close()
				err := apperrors.New(apperrors.ErrBadRequest, "build metadata fields must be sent before archive")
				httpresponse.Respond(c, httpresponse.Status(err), err)
				return
			}
			result, err := h.builds.CreateBuildFromArchive(c.Request.Context(), service.BuildArchiveInput{
				Tag:         tag,
				ContextDir:  contextDir,
				Dockerfile:  dockerfile,
				BuildArgs:   buildArgs,
				ArchiveName: part.FileName(),
				Archive:     part,
			})
			if err != nil {
				_ = part.Close()
				httpresponse.Respond(c, httpresponse.Status(err), err)
				return
			}
			_ = part.Close()
			c.JSON(http.StatusAccepted, gin.H{
				"build_id": result.BuildID.String(),
				"message":  "build initialized",
			})
			return
		}

		value, err := readLimitedFormField(part, maxBuildFormFieldBytes)
		_ = part.Close()
		if err != nil {
			httpresponse.Respond(c, httpresponse.Status(err), err)
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
				if err := json.Unmarshal(value, &buildArgs); err != nil {
					err := apperrors.New(apperrors.ErrBadRequest, "build_args must be a JSON object")
					httpresponse.Respond(c, httpresponse.Status(err), err)
					return
				}
			}
		}
	}

	httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
}

func (h *BuildHandler) BuildImageFromGit(c *gin.Context) {
	var req dto.BuildImageGitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}
	if req.BuildArgs == nil {
		req.BuildArgs = map[string]string{}
	}

	result, err := h.builds.CreateBuildFromGit(c.Request.Context(), service.BuildGitInput{
		RepoURL:    req.RepoURL,
		Ref:        req.Ref,
		Tag:        req.Tag,
		ContextDir: req.Context,
		Dockerfile: req.Dockerfile,
		BuildArgs:  req.BuildArgs,
	})
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"build_id": result.BuildID.String(),
		"message":  "build initialized",
	})
}

func (h *BuildHandler) BuildAvailability(c *gin.Context) {
	cfg := h.cfg.Get()
	message := ""
	if !cfg.ImageBuildsEnabled {
		message = imageBuildsUnavailableMessage
	}
	gitMessage := ""
	if !cfg.GitSourcesEnabled {
		gitMessage = gitSourcesUnavailableMessage
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled":             cfg.ImageBuildsEnabled,
		"message":             message,
		"git_sources_enabled": cfg.GitSourcesEnabled,
		"git_message":         gitMessage,
	})
}

func (h *BuildHandler) BuildLogs(c *gin.Context) {
	buildID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	logReader, err := h.builds.OpenBuildLogs(c.Request.Context(), buildID)
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}
	defer logReader.Close()

	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, logReader)
}

func readLimitedFormField(r io.Reader, maxBytes int64) ([]byte, error) {
	value, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, apperrors.ErrBadRequest
	}
	if int64(len(value)) > maxBytes {
		return nil, apperrors.New(apperrors.ErrBadRequest, "multipart form field is too large")
	}
	return value, nil
}
