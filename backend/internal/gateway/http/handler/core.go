package handler

import (
	"context"
	"errors"
	"net/http"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/domain/dto"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/gin-gonic/gin"
)

type CoreService interface {
	CreateContainer(ctx context.Context, ownerID string, createContainerDTO dto.CreateContainerDTO) (string, error)
	GetUserContainers(ctx context.Context, ownerID string) ([]*coreapi.ContainerData, error)
	ActionContainer(ctx context.Context, ownerID, containerID, action string) error
	ExposeContainer(ctx context.Context, ownerID, containerID, domainPrefix string, internalPort int) error
	CreateVolume(ctx context.Context, ownerID string, createVolumeDTO dto.CreateVolumeDTO) (string, error)
	DeleteVolume(ctx context.Context, ownerID, volumeID string) error
	GetUserVolumes(ctx context.Context, ownerID string) ([]*coreapi.VolumeData, error)
	GetUserImages(ctx context.Context, ownerID string) ([]*coreapi.ImageData, error)
	DeleteImage(ctx context.Context, ownerID, imageID string) error
	GetUserBuilds(ctx context.Context, ownerID string) ([]*coreapi.BuildData, error)
	DeleteBuild(ctx context.Context, ownerID, buildID string) error
	GetUserProjects(ctx context.Context, ownerID string) ([]*coreapi.ProjectData, error)
	DeleteProject(ctx context.Context, ownerID, projectID string) error
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
	var createContainerDTO dto.CreateContainerDTO

	if err := c.ShouldBindJSON(&createContainerDTO); err != nil {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	containerID, err := h.service.CreateContainer(c.Request.Context(), userID, createContainerDTO)
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
		containers = make([]*coreapi.ContainerData, 0)
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

	var exposeContainerDTO dto.ExposeContainerDTO
	if err := c.ShouldBindJSON(&exposeContainerDTO); err != nil {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	err := h.service.ExposeContainer(c.Request.Context(), userID, containerID, exposeContainerDTO.DomainPrefix, exposeContainerDTO.InternalPort)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "container exposed on prefix: " + exposeContainerDTO.DomainPrefix})
}

func (h *CoreHandler) CreateVolume(c *gin.Context) {
	userID := c.GetString("user_id")
	var createVolumeDTO dto.CreateVolumeDTO

	if err := c.ShouldBindJSON(&createVolumeDTO); err != nil {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	volumeID, err := h.service.CreateVolume(c.Request.Context(), userID, createVolumeDTO)
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
		volumes = make([]*coreapi.VolumeData, 0)
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
		images = make([]*coreapi.ImageData, 0)
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

func (h *CoreHandler) GetBuilds(c *gin.Context) {
	userID := c.GetString("user_id")

	builds, err := h.service.GetUserBuilds(c.Request.Context(), userID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	if builds == nil {
		builds = make([]*coreapi.BuildData, 0)
	}

	c.JSON(http.StatusOK, gin.H{"builds": builds})
}

func (h *CoreHandler) DeleteBuild(c *gin.Context) {
	userID := c.GetString("user_id")
	buildID := c.Param("id")

	err := h.service.DeleteBuild(c.Request.Context(), userID, buildID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "build record deleted"})
}

func (h *CoreHandler) GetProjects(c *gin.Context) {
	userID := c.GetString("user_id")

	projects, err := h.service.GetUserProjects(c.Request.Context(), userID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	if projects == nil {
		projects = make([]*coreapi.ProjectData, 0)
	}

	c.JSON(http.StatusOK, gin.H{"projects": projects})
}

func (h *CoreHandler) DeleteProject(c *gin.Context) {
	userID := c.GetString("user_id")
	projectID := c.Param("id")

	err := h.service.DeleteProject(c.Request.Context(), userID, projectID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "project deleted"})
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
	if errors.Is(err, apperrors.ErrBadRequest) {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}
	apperrors.Respond(c, http.StatusInternalServerError, apperrors.ErrInternal)
}
