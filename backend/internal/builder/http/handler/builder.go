package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/domain"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/gin-gonic/gin"
)

const maxUploadSize = 50 << 20

type BuilderService interface {
	InitBuild(ctx context.Context, job domain.BuildJob) (string, error)
}

type BuildHandler struct {
	service BuilderService
}

func NewBuildHandler(service BuilderService) *BuildHandler {
	return &BuildHandler{
		service: service,
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

	file, err := c.FormFile("archive")
	if err != nil {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	job := domain.BuildJob{
		OwnerID: ownerID,
		Tag:     tag,
		File:    file,
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
