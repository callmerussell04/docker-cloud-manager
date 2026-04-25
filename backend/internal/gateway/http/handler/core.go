package handler

import (
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
	apperrors.Respond(c, apperrors.HTTPStatus(err), err)
}
