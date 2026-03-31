package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/dto"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/gin-gonic/gin"
)

type CoreService interface {
	CreateContainer(ctx context.Context, ownerID string, dto dto.CreateContainerDTO) (string, error)
	GetUserContainers(ctx context.Context, ownerID string) ([]*core.ContainerData, error)
	ActionContainer(ctx context.Context, ownerID, containerID, action string) error
	ExposeContainer(ctx context.Context, ownerID, containerID, dtoName string) error
	CreateVolume(ctx context.Context, ownerID string, dto dto.CreateVolumeDTO) (string, error)
	DeleteVolume(ctx context.Context, ownerID, volumeID string) error
	GetUserVolumes(ctx context.Context, ownerID string) ([]*core.VolumeData, error)
	GetUserImages(ctx context.Context, ownerID string) ([]*core.ImageData, error)
	DeleteImage(ctx context.Context, ownerID, imageID string) error
}

type CoreHandler struct {
	service CoreService
}

func NewCoreHandler(service CoreService) *CoreHandler {
	return &CoreHandler{
		service: service,
	}
}

func (h *CoreHandler) CreateContainer(c *gin.Context) {
	userID := c.GetString("user_id")
	var dto dto.CreateContainerDTO

	if err := c.ShouldBindJSON(&dto); err != nil {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	containerID, err := h.service.CreateContainer(c.Request.Context(), userID, dto)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"container_id": containerID})
}

func (h *CoreHandler) GetContainers(c *gin.Context) {
	userID := c.GetString("user_id")

	containers, err := h.service.GetUserContainers(c.Request.Context(), userID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	if containers == nil {
		containers = make([]*core.ContainerData, 0)
	}

	c.JSON(http.StatusOK, gin.H{"containers": containers})
}

func (h *CoreHandler) ActionContainer(c *gin.Context) {
	userID := c.GetString("user_id")
	containerID := c.Param("id")
	action := c.Param("action") // start, stop, delete

	err := h.service.ActionContainer(c.Request.Context(), userID, containerID, action)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": action + " successful"})
}

func (h *CoreHandler) ExposeContainer(c *gin.Context) {
	userID := c.GetString("user_id")
	containerID := c.Param("id")

	var dto dto.ExposeContainerDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	err := h.service.ExposeContainer(c.Request.Context(), userID, containerID, dto.Domain)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "container exposed on dto " + dto.Domain})
}

func (h *CoreHandler) CreateVolume(c *gin.Context) {
	userID := c.GetString("user_id")
	var dto dto.CreateVolumeDTO

	if err := c.ShouldBindJSON(&dto); err != nil {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	volumeID, err := h.service.CreateVolume(c.Request.Context(), userID, dto)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"volume_id": volumeID})
}

func (h *CoreHandler) GetVolumes(c *gin.Context) {
	userID := c.GetString("user_id")

	volumes, err := h.service.GetUserVolumes(c.Request.Context(), userID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	if volumes == nil {
		volumes = make([]*core.VolumeData, 0)
	}

	c.JSON(http.StatusOK, gin.H{"volumes": volumes})
}

func (h *CoreHandler) DeleteVolume(c *gin.Context) {
	userID := c.GetString("user_id")
	volumeID := c.Param("id")

	err := h.service.DeleteVolume(c.Request.Context(), userID, volumeID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "volume deleted"})
}

func (h *CoreHandler) GetImages(c *gin.Context) {
	userID := c.GetString("user_id")

	images, err := h.service.GetUserImages(c.Request.Context(), userID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	if images == nil {
		images = make([]*core.ImageData, 0)
	}

	c.JSON(http.StatusOK, gin.H{"images": images})
}

func (h *CoreHandler) DeleteImage(c *gin.Context) {
	userID := c.GetString("user_id")
	imageID := c.Param("id")

	err := h.service.DeleteImage(c.Request.Context(), userID, imageID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "image deleted"})
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
	if errors.Is(err, apperrors.ErrBadRequest) {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}
	apperrors.Respond(c, http.StatusInternalServerError, apperrors.ErrInternal)
}
