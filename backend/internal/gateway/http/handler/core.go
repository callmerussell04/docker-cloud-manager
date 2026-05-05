package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

func getPaginationParams(c *gin.Context) (int, int, bool) {
	pageRaw := c.DefaultQuery("page", "1")
	page, err := strconv.Atoi(pageRaw)
	if err != nil || page < 1 {
		badRequest(c, "page must be a positive integer")
		return 0, 0, false
	}
	limitRaw := c.DefaultQuery("limit", "20")
	limit, err := strconv.Atoi(limitRaw)
	if err != nil || limit < 1 || limit > 100 {
		badRequest(c, "limit must be between 1 and 100")
		return 0, 0, false
	}
	return page, limit, true
}

func (h *CoreHandler) handleError(c *gin.Context, err error) {
	httpresponse.Respond(c, httpresponse.Status(err), err)
}

func pathUUID(c *gin.Context, name string) (string, bool) {
	value := c.Param(name)
	return validateUUIDValue(c, name, value)
}

func validateUUIDValue(c *gin.Context, name, value string) (string, bool) {
	if _, err := uuid.Parse(value); err != nil {
		badRequest(c, fmt.Sprintf("%s must be a valid UUID", name))
		return "", false
	}
	return value, true
}

func validateContainerAction(c *gin.Context, action string) bool {
	switch action {
	case "start", "stop", "delete":
		return true
	default:
		badRequest(c, "action must be one of: start, stop, delete")
		return false
	}
}

func badRequest(c *gin.Context, message string) {
	httpresponse.Respond(c, http.StatusBadRequest, apperrors.New(apperrors.ErrBadRequest, message))
}
