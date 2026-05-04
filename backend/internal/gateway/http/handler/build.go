package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/buildobjects"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type BuildService interface {
	CreateBuildJob(ctx context.Context, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (string, string, error)
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
	if h.objectStore == nil {
		err := apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}

	cfg, err := h.service.GetSystemConfig(c.Request.Context())
	if err != nil {
		h.handleError(c, err)
		return
	}
	if !cfg.ImageBuildsEnabled {
		err := apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
		httpresponse.Respond(c, httpresponse.Status(err), err)
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
			if err := h.createBuildFromArchivePart(c, tag, contextDir, dockerfile, buildArgs, part.FileName(), part, cfg.MaxArchiveSizeBytes); err != nil {
				_ = part.Close()
				httpresponse.Respond(c, httpresponse.Status(err), err)
				return
			}
			_ = part.Close()
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

func (h *CoreHandler) createBuildFromArchivePart(c *gin.Context, tag, contextDir, dockerfile string, buildArgs map[string]string, archiveName string, archive io.Reader, maxArchiveSize int64) error {
	if err := validation.ImageTag(tag); err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	if err := validateRelativeBuildPath(contextDir); err != nil {
		return fmt.Errorf("%w: invalid build context: %v", apperrors.ErrBadRequest, err)
	}
	if err := validateRelativeBuildPath(dockerfile); err != nil {
		return fmt.Errorf("%w: invalid dockerfile path: %v", apperrors.ErrBadRequest, err)
	}

	fileID := uuid.New().String()
	archiveObjectKey := buildobjects.ArchiveObjectKey(fileID, archiveName)
	logObjectKey := buildobjects.LogObjectKey(fileID)

	limitedArchive := &maxBytesReader{r: archive, remaining: maxArchiveSize}
	if err := h.objectStore.UploadStream(c.Request.Context(), archiveObjectKey, limitedArchive, -1, "application/octet-stream"); err != nil {
		_ = h.objectStore.DeleteObject(context.Background(), archiveObjectKey)
		if errors.Is(err, apperrors.ErrBadRequest) {
			return apperrors.New(apperrors.ErrBadRequest, "archive exceeds configured size limit")
		}
		return err
	}

	buildID, _, err := h.service.CreateBuildJob(
		c.Request.Context(),
		tag,
		archiveObjectKey,
		logObjectKey,
		contextDir,
		dockerfile,
		buildArgs,
		logging.RequestIDFromContext(c.Request.Context()),
	)
	if err != nil {
		_ = h.objectStore.DeleteObject(context.Background(), archiveObjectKey)
		return err
	}

	args := []any{
		"request_id", logging.RequestIDFromContext(c.Request.Context()),
		"build_id", buildID,
		"archive_object_key", archiveObjectKey,
		"log_object_key", logObjectKey,
	}
	if scope, ok := accessscope.FromContext(c.Request.Context()); ok {
		args = append(args, "user_id", scope.UserID)
	}
	slog.InfoContext(c.Request.Context(), "build upload accepted", args...)

	c.JSON(http.StatusAccepted, gin.H{
		"build_id": buildID,
		"message":  "build initialized",
	})
	return nil
}

func (h *CoreHandler) GetBuildLogs(c *gin.Context) {
	if h.objectStore == nil {
		err := apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}
	buildID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	build, err := h.service.GetBuild(c.Request.Context(), buildID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	if build.LogFilePath == "" {
		httpresponse.Respond(c, httpresponse.Status(apperrors.ErrNotFound), apperrors.ErrNotFound)
		return
	}
	logReader, err := h.objectStore.OpenObject(c.Request.Context(), build.LogFilePath)
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

func validateRelativeBuildPath(path string) error {
	if path == "" || path == "." {
		return nil
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("absolute paths are not allowed")
	}
	clean := filepath.Clean(path)
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "\x00") {
		return fmt.Errorf("parent directory traversal is not allowed")
	}
	return nil
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

type maxBytesReader struct {
	r         io.Reader
	remaining int64
}

func (r *maxBytesReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		var one [1]byte
		n, err := r.r.Read(one[:])
		if n > 0 {
			return 0, apperrors.ErrBadRequest
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	return n, err
}
