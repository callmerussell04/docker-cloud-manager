package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/gin-gonic/gin"
)

type BuildService interface {
	CreateBuildJob(ctx context.Context, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (string, string, error)
	CreateBuildFromArchive(ctx context.Context, input model.BuildArchiveInput) (model.BuildInitResult, error)
	CreateBuildFromGit(ctx context.Context, input model.BuildGitInput) (model.BuildInitResult, error)
	OpenBuildLogs(ctx context.Context, buildID string) (io.ReadCloser, error)
	GetBuild(ctx context.Context, buildID string) (model.Build, error)
	CancelBuildRecord(ctx context.Context, buildID string) error
	DeleteBuild(ctx context.Context, buildID string) error
	GetAllBuilds(ctx context.Context, page, limit int) (model.PaginatedBuilds, error)
}

const imageBuildsUnavailableMessage = "Image builds are currently unavailable. Use Docker Hub images."

const maxBuildFormFieldBytes = 1 << 20

func (h *CoreHandler) GetBuilds(c *gin.Context) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	resp, err := h.service.GetAllBuilds(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}

	builds := resp.Builds
	if builds == nil {
		builds = []model.Build{}
	}

	c.JSON(http.StatusOK, gin.H{
		"builds":      buildsToUserDTO(builds),
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) BuildImage(c *gin.Context) {
	cfg, err := h.service.GetSystemConfig(c.Request.Context())
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, cfg.MaxUploadSizeBytes)

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
			result, err := h.service.CreateBuildFromArchive(c.Request.Context(), model.BuildArchiveInput{
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
				"build_id": result.BuildID,
				"message":  "build initialized",
			})
			return
		}

		value, err := readBuildFormField(part, maxBuildFormFieldBytes)
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

func (h *CoreHandler) BuildImageFromGit(c *gin.Context) {
	var req dto.BuildImageGitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}
	if req.BuildArgs == nil {
		req.BuildArgs = map[string]string{}
	}

	result, err := h.service.CreateBuildFromGit(c.Request.Context(), model.BuildGitInput{
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
		"build_id": result.BuildID,
		"message":  "build initialized",
	})
}

func (h *CoreHandler) GetImageBuildAvailability(c *gin.Context) {
	cfg, err := h.service.GetSystemConfig(c.Request.Context())
	if err != nil {
		h.handleError(c, err)
		return
	}

	message := ""
	if !cfg.ImageBuildsEnabled {
		message = imageBuildsUnavailableMessage
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled": cfg.ImageBuildsEnabled,
		"message": message,
	})
}

func (h *CoreHandler) GetBuildLogs(c *gin.Context) {
	buildID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	logReader, err := h.service.OpenBuildLogs(c.Request.Context(), buildID)
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}
	defer logReader.Close()

	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, logReader)
}

func (h *CoreHandler) CancelBuild(c *gin.Context) {
	buildID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	if err := h.service.CancelBuildRecord(c.Request.Context(), buildID); err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "build cancellation requested"})
}

func (h *CoreHandler) AdminGetBuildLogs(c *gin.Context) {
	h.GetBuildLogs(c)
}

func (h *CoreHandler) AdminCancelBuild(c *gin.Context) {
	h.CancelBuild(c)
}

func (h *CoreHandler) DeleteBuild(c *gin.Context) {
	buildID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	err := h.service.DeleteBuild(c.Request.Context(), buildID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "build record deleted"})
}

func (h *CoreHandler) GetAllBuilds(c *gin.Context) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	resp, err := h.service.GetAllBuilds(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"builds":      buildsToDTO(resp.Builds),
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) AdminDeleteBuild(c *gin.Context) {
	buildID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	err := h.service.DeleteBuild(c.Request.Context(), buildID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "build deleted by admin"})
}

func readBuildFormField(r io.Reader, maxBytes int64) ([]byte, error) {
	value, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, apperrors.ErrBadRequest
	}
	if int64(len(value)) > maxBytes {
		return nil, apperrors.New(apperrors.ErrBadRequest, "multipart form field is too large")
	}
	return value, nil
}
