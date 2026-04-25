package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/gin-gonic/gin"
)

type CoreService interface {
	ContainerService
	VolumeService
	ImageService
	BuildService
	ProjectService
	SystemService
	StatsService
}

type CoreHandler struct {
	service CoreService
}

func NewCoreHandler(service CoreService) *CoreHandler {
	return &CoreHandler{
		service: service,
	}
}

func getPaginationParams(c *gin.Context) (int, int) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil || limit < 1 || limit > 100 {
		limit = 20
	}
	return page, limit
}

func (h *CoreHandler) handleError(c *gin.Context, err error) {
	if errors.Is(err, apperrors.ErrNotFound) {
		apperrors.Respond(c, http.StatusNotFound, apperrors.ErrNotFound)
		return
	}
	if errors.Is(err, apperrors.ErrAlreadyExists) {
		apperrors.Respond(c, http.StatusConflict, apperrors.ErrAlreadyExists)
		return
	}
	if errors.Is(err, apperrors.ErrLimitExceeded) {
		apperrors.Respond(c, http.StatusConflict, apperrors.ErrLimitExceeded)
		return
	}
	if errors.Is(err, apperrors.ErrResourceExhausted) || errors.Is(err, apperrors.ErrQuotaExceeded) || errors.Is(err, apperrors.ErrHostExhausted) {
		apperrors.Respond(c, http.StatusConflict, apperrors.ErrResourceExhausted)
		return
	}
	if errors.Is(err, apperrors.ErrUnauthorized) {
		apperrors.Respond(c, http.StatusUnauthorized, apperrors.ErrUnauthorized)
		return
	}
	if errors.Is(err, apperrors.ErrBadRequest) {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}
	apperrors.Respond(c, http.StatusInternalServerError, apperrors.ErrInternal)
}
