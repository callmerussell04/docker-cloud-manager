package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/gin-gonic/gin"
)

type ContainerService interface {
	CreateContainer(ctx context.Context, ownerID string, createContainerDTO dto.CreateContainerDTO) (string, error)
	GetUserContainers(ctx context.Context, ownerID string) ([]dto.ContainerDTO, error)
	ActionContainer(ctx context.Context, ownerID, containerID, action string) error
	ExposeContainer(ctx context.Context, ownerID, containerID, domainPrefix string, internalPort int) error
	GetAllContainers(ctx context.Context, page, limit int) (dto.PaginatedContainers, error)
	AdminActionContainer(ctx context.Context, containerID, action string) error
	GetContainerStats(ctx context.Context, ownerID, containerID string) (dto.ContainerStatsDTO, error)
	AdminGetContainerStats(ctx context.Context, containerID string) (dto.ContainerStatsDTO, error)
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
		containers = make([]dto.ContainerDTO, 0)
	}

	c.JSON(http.StatusOK, gin.H{"containers": containers})
}

func (h *CoreHandler) ActionContainer(c *gin.Context) {
	userID := c.GetString("user_id")
	containerID := c.Param("id")
	action := c.Param("action")

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

func (h *CoreHandler) GetAllContainers(c *gin.Context) {
	page, limit := getPaginationParams(c)
	resp, err := h.service.GetAllContainers(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"containers":  resp.Containers,
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) AdminActionContainer(c *gin.Context) {
	containerID := c.Param("id")
	action := c.Param("action")

	err := h.service.AdminActionContainer(c.Request.Context(), containerID, action)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "admin action " + action + " successful"})
}

func (h *CoreHandler) GetContainerStats(c *gin.Context) {
	userID := c.GetString("user_id")
	containerID := c.Param("id")

	stats, err := h.service.GetContainerStats(c.Request.Context(), userID, containerID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, stats)
}

func (h *CoreHandler) AdminGetContainerStats(c *gin.Context) {
	containerID := c.Param("id")

	stats, err := h.service.AdminGetContainerStats(c.Request.Context(), containerID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, stats)
}
