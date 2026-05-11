package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
	"github.com/gin-gonic/gin"
)

type ContainerService interface {
	CreateContainer(ctx context.Context, input model.CreateContainerInput) (string, error)
	ActionContainer(ctx context.Context, containerID, action string) error
	ExposeContainer(ctx context.Context, containerID, domainPrefix string, internalPort int) error
	ListContainers(ctx context.Context, page, limit int) (model.PaginatedContainers, error)
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
	outcome, errorCode := auditOutcome(err)
	h.recordAudit(c, auditInput{
		Action:       auditlog.ActionContainerCreate,
		ResourceType: auditlog.ResourceContainer,
		ResourceID:   containerID,
		ResourceName: createContainerDTO.Name,
		Outcome:      outcome,
		ErrorCode:    errorCode,
		DetailsJSON:  auditlog.SafeDetailsJSON(map[string]string{auditlog.DetailDomainPrefix: createContainerDTO.DomainPrefix, auditlog.DetailInternalPort: auditlog.IntDetail(createContainerDTO.InternalPort)}),
	})
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"container_id": containerID})
}

func (h *CoreHandler) GetContainers(c *gin.Context) {
	h.listContainers(c, func(items []model.Container) any {
		return containersToUserDTO(items)
	})
}

func (h *CoreHandler) ListAdminContainers(c *gin.Context) {
	h.listContainers(c, func(items []model.Container) any {
		return containersToDTO(items)
	})
}

func (h *CoreHandler) listContainers(c *gin.Context, mapItems func([]model.Container) any) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}
	resp, err := h.service.ListContainers(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"containers":  mapItems(resp.Containers),
		"total_count": resp.TotalCount,
	})
}

func (h *CoreHandler) ActionContainer(c *gin.Context) {
	h.runContainerAction(c, func(action string) string {
		return action + " successful"
	})
}

func (h *CoreHandler) AdminActionContainer(c *gin.Context) {
	h.runContainerAction(c, func(action string) string {
		return "admin action " + action + " successful"
	})
}

func (h *CoreHandler) runContainerAction(c *gin.Context, message func(string) string) {
	containerID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	action := c.Param("action")
	if !validateContainerAction(c, action) {
		return
	}

	err := h.service.ActionContainer(c.Request.Context(), containerID, action)
	outcome, errorCode := auditOutcome(err)
	h.recordAudit(c, auditInput{
		Action:       containerAuditAction(action),
		ResourceType: auditlog.ResourceContainer,
		ResourceID:   containerID,
		Outcome:      outcome,
		ErrorCode:    errorCode,
	})
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": message(action)})
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
	outcome, errorCode := auditOutcome(err)
	h.recordAudit(c, auditInput{
		Action:       auditlog.ActionContainerExpose,
		ResourceType: auditlog.ResourceContainer,
		ResourceID:   containerID,
		Outcome:      outcome,
		ErrorCode:    errorCode,
		DetailsJSON:  auditlog.SafeDetailsJSON(map[string]string{auditlog.DetailDomainPrefix: exposeContainerDTO.DomainPrefix, auditlog.DetailInternalPort: auditlog.IntDetail(exposeContainerDTO.InternalPort)}),
	})
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "container exposed on prefix: " + exposeContainerDTO.DomainPrefix})
}

func containerAuditAction(action string) string {
	switch action {
	case "start":
		return auditlog.ActionContainerStart
	case "stop":
		return auditlog.ActionContainerStop
	case "delete":
		return auditlog.ActionContainerDelete
	default:
		return "container." + action
	}
}

func (h *CoreHandler) GetContainerStats(c *gin.Context) {
	h.writeContainerStats(c)
}

func (h *CoreHandler) AdminGetContainerStats(c *gin.Context) {
	h.writeContainerStats(c)
}

func (h *CoreHandler) writeContainerStats(c *gin.Context) {
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
