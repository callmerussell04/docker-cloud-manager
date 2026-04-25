package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/gin-gonic/gin"
)

type ContainerService interface {
	CreateContainer(ctx context.Context, ownerID string, input model.CreateContainerInput) (string, error)
	GetUserContainers(ctx context.Context, ownerID string) ([]model.Container, error)
	ActionContainer(ctx context.Context, ownerID, containerID, action string) error
	ExposeContainer(ctx context.Context, ownerID, containerID, domainPrefix string, internalPort int) error
	GetAllContainers(ctx context.Context, page, limit int) (model.PaginatedContainers, error)
	AdminActionContainer(ctx context.Context, containerID, action string) error
	GetContainerStats(ctx context.Context, ownerID, containerID string) (model.ContainerStats, error)
	AdminGetContainerStats(ctx context.Context, containerID string) (model.ContainerStats, error)
}

func (h *CoreHandler) CreateContainer(c *gin.Context) {
	userID := c.GetString("user_id")
	var createContainerDTO dto.CreateContainerDTO

	if err := c.ShouldBindJSON(&createContainerDTO); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	containerID, err := h.service.CreateContainer(c.Request.Context(), userID, createContainerInputFromDTO(createContainerDTO))
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
		containers = make([]model.Container, 0)
	}

	c.JSON(http.StatusOK, gin.H{"containers": containersToDTO(containers)})
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
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
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
		"containers":  containersToDTO(resp.Containers),
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

	c.JSON(http.StatusOK, containerStatsToDTO(stats))
}

func (h *CoreHandler) AdminGetContainerStats(c *gin.Context) {
	containerID := c.Param("id")

	stats, err := h.service.AdminGetContainerStats(c.Request.Context(), containerID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, containerStatsToDTO(stats))
}
