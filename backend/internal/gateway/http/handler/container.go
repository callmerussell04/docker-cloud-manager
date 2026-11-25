package handler

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/httpresponse"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/validation"
	"github.com/gin-gonic/gin"
)

type ContainerService interface {
	CreateContainer(ctx context.Context, input model.CreateContainerInput) (string, error)
	ActionContainer(ctx context.Context, containerID, action string) error
	ExposeContainer(ctx context.Context, containerID, domainPrefix string, internalPort int) error
	ListContainers(ctx context.Context, params model.ContainerListParams) (model.PaginatedContainers, error)
	GetContainer(ctx context.Context, containerID string) (model.Container, error)
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

	c.JSON(http.StatusAccepted, gin.H{"container_id": containerID})
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
	params, ok := containerListParams(c)
	if !ok {
		return
	}
	resp, err := h.service.ListContainers(c.Request.Context(), params)
	if err != nil {
		h.handleError(c, err)
		return
	}

	counts := resp.StatusCounts
	c.JSON(http.StatusOK, gin.H{
		"containers":  mapItems(resp.Containers),
		"total_count": resp.TotalCount,
		"status_counts": dto.ContainerStatusCountsDTO{
			All:       counts.All,
			Running:   counts.Running,
			Stopped:   counts.Stopped,
			Attention: counts.Attention,
		},
	})
}

// containerSearchMaxLength is the longest containers list search, in
// characters, after trimming.
const containerSearchMaxLength = 100

// containerListParams reads the containers list's query: the page, the
// project, and the status filter, sort, order and search, each left empty
// when omitted. A value it does not know is a 400 that does not repeat it.
func containerListParams(c *gin.Context) (model.ContainerListParams, bool) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return model.ContainerListParams{}, false
	}
	params := model.ContainerListParams{
		Page:      page,
		Limit:     limit,
		ProjectID: c.Query("project_id"),
		Status:    c.Query("status"),
		Sort:      c.Query("sort"),
		Order:     c.Query("order"),
		Query:     strings.TrimSpace(c.Query("q")),
	}
	if params.ProjectID != "" {
		if _, ok := validateUUIDValue(c, "project_id", params.ProjectID); !ok {
			return model.ContainerListParams{}, false
		}
	}
	checks := []struct {
		value, message string
		allowed        []string
	}{
		{params.Status, "status must be one of: all, running, stopped, attention", []string{"all", "running", "stopped", "attention"}},
		{params.Sort, "sort must be one of: name, status, address, memory, created", []string{"name", "status", "address", "memory", "created"}},
		{params.Order, "order must be asc or desc", []string{"asc", "desc"}},
	}
	for _, check := range checks {
		if check.value != "" && !slices.Contains(check.allowed, check.value) {
			badRequest(c, check.message)
			return model.ContainerListParams{}, false
		}
	}
	if utf8.RuneCountInString(params.Query) > containerSearchMaxLength {
		badRequest(c, "q must be at most 100 characters")
		return model.ContainerListParams{}, false
	}
	return params, true
}

func (h *CoreHandler) GetContainer(c *gin.Context) {
	h.writeContainer(c, func(item model.Container) any {
		return containerToUserDTO(item)
	})
}

func (h *CoreHandler) AdminGetContainer(c *gin.Context) {
	h.writeContainer(c, func(item model.Container) any {
		return containerToDTO(item)
	})
}

func (h *CoreHandler) writeContainer(c *gin.Context, mapItem func(model.Container) any) {
	containerID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	container, err := h.service.GetContainer(c.Request.Context(), containerID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, mapItem(container))
}

func (h *CoreHandler) ActionContainer(c *gin.Context) {
	h.runContainerAction(c, func(action string) string {
		return action + " requested"
	})
}

func (h *CoreHandler) AdminActionContainer(c *gin.Context) {
	h.runContainerAction(c, func(action string) string {
		return "admin action " + action + " requested"
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

	c.JSON(http.StatusAccepted, gin.H{"message": message(action)})
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

	c.JSON(http.StatusAccepted, gin.H{"message": "container expose requested for prefix: " + exposeContainerDTO.DomainPrefix})
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
