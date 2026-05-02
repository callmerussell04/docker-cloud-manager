package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
	"github.com/gin-gonic/gin"
)

type ContainerService interface {
	CreateContainer(ctx context.Context, input model.CreateContainerInput) (string, error)
	ActionContainer(ctx context.Context, containerID, action string) error
	ExposeContainer(ctx context.Context, containerID, domainPrefix string, internalPort int) error
	GetAllContainers(ctx context.Context, page, limit int) (model.PaginatedContainers, error)
	GetContainerStats(ctx context.Context, containerID string) (model.ContainerStats, error)
}

func (h *CoreHandler) CreateContainer(c *gin.Context) {
	var createContainerDTO dto.CreateContainerDTO

	if err := c.ShouldBindJSON(&createContainerDTO); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}
	if !validateCreateContainerDTO(c, createContainerDTO) {
		return
	}

	containerID, err := h.service.CreateContainer(c.Request.Context(), createContainerInputFromDTO(createContainerDTO))
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"container_id": containerID})
}

func (h *CoreHandler) GetContainers(c *gin.Context) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	resp, err := h.service.GetAllContainers(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}

	containers := resp.Containers
	if containers == nil {
		containers = []model.Container{}
	}

	c.JSON(http.StatusOK, gin.H{
		"containers":  containersToUserDTO(containers),
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) ActionContainer(c *gin.Context) {
	containerID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	action := c.Param("action")
	if !validateContainerAction(c, action) {
		return
	}

	err := h.service.ActionContainer(c.Request.Context(), containerID, action)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": action + " successful"})
}

func (h *CoreHandler) ExposeContainer(c *gin.Context) {
	containerID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	var exposeContainerDTO dto.ExposeContainerDTO
	if err := c.ShouldBindJSON(&exposeContainerDTO); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}
	if !validateExposeContainerDTO(c, exposeContainerDTO) {
		return
	}

	err := h.service.ExposeContainer(c.Request.Context(), containerID, exposeContainerDTO.DomainPrefix, exposeContainerDTO.InternalPort)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "container exposed on prefix: " + exposeContainerDTO.DomainPrefix})
}

func (h *CoreHandler) GetAllContainers(c *gin.Context) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	resp, err := h.service.GetAllContainers(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"containers":  containersToDTO(resp.Containers),
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) AdminActionContainer(c *gin.Context) {
	containerID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	action := c.Param("action")
	if !validateContainerAction(c, action) {
		return
	}

	err := h.service.ActionContainer(c.Request.Context(), containerID, action)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "admin action " + action + " successful"})
}

func (h *CoreHandler) GetContainerStats(c *gin.Context) {
	containerID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	stats, err := h.service.GetContainerStats(c.Request.Context(), containerID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, containerStatsToDTO(stats))
}

func (h *CoreHandler) AdminGetContainerStats(c *gin.Context) {
	containerID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	stats, err := h.service.GetContainerStats(c.Request.Context(), containerID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, containerStatsToDTO(stats))
}

func validateCreateContainerDTO(c *gin.Context, req dto.CreateContainerDTO) bool {
	if err := validation.ResourceName(req.Name); err != nil {
		badRequest(c, err.Error())
		return false
	}
	if err := validation.ImageTag(req.ImageTag); err != nil {
		badRequest(c, err.Error())
		return false
	}
	if req.InternalPort < 0 || req.InternalPort > 65535 {
		badRequest(c, "internal_port must be between 0 and 65535")
		return false
	}
	if err := validation.DomainPrefix(req.DomainPrefix); err != nil {
		badRequest(c, err.Error())
		return false
	}
	if req.DomainPrefix != "" && req.InternalPort <= 0 {
		badRequest(c, "internal_port is required when domain_prefix is set")
		return false
	}
	for _, mount := range req.VolumeMounts {
		if _, ok := validateUUIDValue(c, "volume_id", mount.VolumeID); !ok {
			return false
		}
		if err := validation.MountPath(mount.MountPath); err != nil {
			badRequest(c, err.Error())
			return false
		}
	}
	return true
}

func validateExposeContainerDTO(c *gin.Context, req dto.ExposeContainerDTO) bool {
	if err := validation.DomainPrefix(req.DomainPrefix); err != nil {
		badRequest(c, err.Error())
		return false
	}
	if req.InternalPort <= 0 || req.InternalPort > 65535 {
		badRequest(c, "internal_port must be between 1 and 65535")
		return false
	}
	return true
}
