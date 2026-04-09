package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

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
	StopProject(ctx context.Context, ownerID, projectID string) error
	GetAllContainers(ctx context.Context, page, limit int) (*coreapi.PaginatedContainerResponse, error)
	AdminActionContainer(ctx context.Context, containerID, action string) error
	GetAllVolumes(ctx context.Context, page, limit int) (*coreapi.PaginatedVolumeResponse, error)
	AdminDeleteVolume(ctx context.Context, volumeID string) error
	GetAllImages(ctx context.Context, page, limit int) (*coreapi.PaginatedImageResponse, error)
	AdminDeleteImage(ctx context.Context, imageID string) error
	GetAllBuilds(ctx context.Context, page, limit int) (*coreapi.PaginatedBuildResponse, error)
	AdminDeleteBuild(ctx context.Context, buildID string) error
	GetAllProjects(ctx context.Context, page, limit int) (*coreapi.PaginatedProjectResponse, error)
	AdminDeleteProject(ctx context.Context, projectID string) error
	AdminStopProject(ctx context.Context, projectID string) error
	GetSystemConfig(ctx context.Context) (*coreapi.SystemConfigData, error)
	UpdateSystemConfig(ctx context.Context, req *coreapi.SystemConfigData) error
	GetContainerStats(ctx context.Context, ownerID, containerID string) (*coreapi.ContainerStatsResponse, error)
	AdminGetContainerStats(ctx context.Context, containerID string) (*coreapi.ContainerStatsResponse, error)
	GetUserStats(ctx context.Context, ownerID string) (*coreapi.UserStatsResponse, error)
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

func (h *CoreHandler) StopProject(c *gin.Context) {
	userID := c.GetString("user_id")
	projectID := c.Param("id")

	err := h.service.StopProject(c.Request.Context(), userID, projectID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "project stopped successfully"})
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

// === ADMIN HANDLERS ===

func (h *CoreHandler) GetAllContainers(c *gin.Context) {
	page, limit := getPaginationParams(c)
	resp, err := h.service.GetAllContainers(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"containers":  resp.GetContainers(),
		"total_count": resp.GetTotalCount(),
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

func (h *CoreHandler) GetAllVolumes(c *gin.Context) {
	page, limit := getPaginationParams(c)
	resp, err := h.service.GetAllVolumes(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"volumes":     resp.GetVolumes(),
		"total_count": resp.GetTotalCount(),
	})
}

func (h *CoreHandler) AdminDeleteVolume(c *gin.Context) {
	volumeID := c.Param("id")
	err := h.service.AdminDeleteVolume(c.Request.Context(), volumeID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "volume deleted by admin"})
}

func (h *CoreHandler) GetAllImages(c *gin.Context) {
	page, limit := getPaginationParams(c)
	resp, err := h.service.GetAllImages(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"images":      resp.GetImages(),
		"total_count": resp.GetTotalCount(),
	})
}

func (h *CoreHandler) AdminDeleteImage(c *gin.Context) {
	imageID := c.Param("id")
	err := h.service.AdminDeleteImage(c.Request.Context(), imageID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "image deleted by admin"})
}

func (h *CoreHandler) GetAllBuilds(c *gin.Context) {
	page, limit := getPaginationParams(c)
	resp, err := h.service.GetAllBuilds(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"builds":      resp.GetBuilds(),
		"total_count": resp.GetTotalCount(),
	})
}

func (h *CoreHandler) AdminDeleteBuild(c *gin.Context) {
	buildID := c.Param("id")
	err := h.service.AdminDeleteBuild(c.Request.Context(), buildID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "build deleted by admin"})
}

func (h *CoreHandler) GetAllProjects(c *gin.Context) {
	page, limit := getPaginationParams(c)
	resp, err := h.service.GetAllProjects(c.Request.Context(), page, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"projects":    resp.GetProjects(),
		"total_count": resp.GetTotalCount(),
	})
}

func (h *CoreHandler) AdminDeleteProject(c *gin.Context) {
	projectID := c.Param("id")
	err := h.service.AdminDeleteProject(c.Request.Context(), projectID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "project deleted by admin"})
}

func (h *CoreHandler) AdminStopProject(c *gin.Context) {
	projectID := c.Param("id")
	err := h.service.AdminStopProject(c.Request.Context(), projectID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "project stopped by admin"})
}

func (h *CoreHandler) GetSystemConfig(c *gin.Context) {
	resp, err := h.service.GetSystemConfig(c.Request.Context())
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *CoreHandler) UpdateSystemConfig(c *gin.Context) {
	var systemConfigDTO dto.SystemConfigDTO
	if err := c.ShouldBindJSON(&systemConfigDTO); err != nil {
		apperrors.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}

	req := &coreapi.SystemConfigData{
		BaseDomain:                    systemConfigDTO.BaseDomain,
		DefaultMemoryReservationBytes: systemConfigDTO.DefaultMemoryReservationBytes,
		ReservedSystemMemoryBytes:     systemConfigDTO.ReservedSystemMemoryBytes,
		OvercommitFactor:              systemConfigDTO.OvercommitFactor,
		MaxBurstMultiplier:            systemConfigDTO.MaxBurstMultiplier,
		DefaultCpuShares:              systemConfigDTO.DefaultCpuShares,
		HighLoadCpuShares:             systemConfigDTO.HighLoadCpuShares,
		HighLoadContainerCount:        int32(systemConfigDTO.HighLoadContainerCount),
		ContainerStopTimeout:          int32(systemConfigDTO.ContainerStopTimeout),
		MaxLogSize:                    systemConfigDTO.MaxLogSize,
		MaxLogFiles:                   systemConfigDTO.MaxLogFiles,
		ContainerDiskQuota:            systemConfigDTO.ContainerDiskQuota,
		MaxVolumesPerUser:             int32(systemConfigDTO.MaxVolumesPerUser),
		MaxContainersPerUser:          int32(systemConfigDTO.MaxContainersPerUser),
		RegistryUrl:                   systemConfigDTO.RegistryUrl,
		ContainerTtlHours:             systemConfigDTO.ContainerTtlHours,
	}

	err := h.service.UpdateSystemConfig(c.Request.Context(), req)
	if err != nil {
		h.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "config updated"})
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

func (h *CoreHandler) GetUserStats(c *gin.Context) {
	userID := c.GetString("user_id")

	stats, err := h.service.GetUserStats(c.Request.Context(), userID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, stats)
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
