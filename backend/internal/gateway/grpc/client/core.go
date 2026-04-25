package grpcclient

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type CoreClient struct {
	containerAPI coreapi.ContainerAPIClient
	imageAPI     coreapi.ImageAPIClient
	volumeAPI    coreapi.VolumeAPIClient
	projectAPI   coreapi.ProjectAPIClient
	systemAPI    coreapi.SystemAPIClient
	statsAPI     coreapi.StatsAPIClient
}

func NewCoreClient(cc *grpc.ClientConn) *CoreClient {
	return &CoreClient{
		containerAPI: coreapi.NewContainerAPIClient(cc),
		imageAPI:     coreapi.NewImageAPIClient(cc),
		volumeAPI:    coreapi.NewVolumeAPIClient(cc),
		projectAPI:   coreapi.NewProjectAPIClient(cc),
		systemAPI:    coreapi.NewSystemAPIClient(cc),
		statsAPI:     coreapi.NewStatsAPIClient(cc),
	}
}

func (c *CoreClient) CreateContainer(ctx context.Context, ownerID string, createContainerDTO dto.CreateContainerDTO) (string, error) {
	var mounts []*coreapi.VolumeMount
	for _, m := range createContainerDTO.VolumeMounts {
		mounts = append(mounts, &coreapi.VolumeMount{
			VolumeId:   m.VolumeID,
			MountPath:  m.MountPath,
			IsReadonly: m.IsReadOnly,
		})
	}

	req := &coreapi.CreateContainerRequest{
		OwnerId:      ownerID,
		Name:         createContainerDTO.Name,
		ImageTag:     createContainerDTO.ImageTag,
		InternalPort: int32(createContainerDTO.InternalPort),
		EnvVars:      createContainerDTO.EnvVars,
		VolumeMounts: mounts,
		DomainPrefix: createContainerDTO.DomainPrefix,
	}

	resp, err := c.containerAPI.CreateContainer(ctx, req)
	if err != nil {
		return "", mapCoreError(err)
	}

	return resp.GetContainerId(), nil
}

func (c *CoreClient) GetUserContainers(ctx context.Context, ownerID string) ([]dto.ContainerDTO, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.containerAPI.GetUserContainers(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return containersFromProto(resp.GetContainers()), nil
}

func (c *CoreClient) ActionContainer(ctx context.Context, ownerID, containerID, action string) error {
	req := &coreapi.ContainerActionRequest{
		OwnerId:     ownerID,
		ContainerId: containerID,
	}

	var err error
	switch action {
	case "start":
		_, err = c.containerAPI.StartContainer(ctx, req)
	case "stop":
		_, err = c.containerAPI.StopContainer(ctx, req)
	case "delete":
		_, err = c.containerAPI.DeleteContainer(ctx, req)
	default:
		return apperrors.ErrBadRequest
	}

	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) ExposeContainer(ctx context.Context, ownerID, containerID, domainPrefix string, internalPort int) error {
	req := &coreapi.ExposeRequest{
		OwnerId:      ownerID,
		ContainerId:  containerID,
		DomainPrefix: domainPrefix,
		InternalPort: int32(internalPort),
	}
	_, err := c.containerAPI.ExposeContainer(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) CreateVolume(ctx context.Context, ownerID string, createVolumeDTO dto.CreateVolumeDTO) (string, error) {
	req := &coreapi.CreateVolumeRequest{
		OwnerId: ownerID,
		Name:    createVolumeDTO.Name,
	}
	resp, err := c.volumeAPI.CreateVolume(ctx, req)
	if err != nil {
		return "", mapCoreError(err)
	}
	return resp.GetVolumeId(), nil
}

func (c *CoreClient) DeleteVolume(ctx context.Context, ownerID, volumeID string) error {
	req := &coreapi.VolumeActionRequest{
		OwnerId:  ownerID,
		VolumeId: volumeID,
	}
	_, err := c.volumeAPI.DeleteVolume(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetUserVolumes(ctx context.Context, ownerID string) ([]dto.VolumeDTO, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.volumeAPI.GetUserVolumes(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return volumesFromProto(resp.GetVolumes()), nil
}

func (c *CoreClient) GetUserImages(ctx context.Context, ownerID string) ([]dto.ImageDTO, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.imageAPI.GetUserImages(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return imagesFromProto(resp.GetImages()), nil
}

func (c *CoreClient) DeleteImage(ctx context.Context, ownerID, imageID string) error {
	req := &coreapi.ImageActionRequest{
		OwnerId: ownerID,
		ImageId: imageID,
	}
	_, err := c.imageAPI.DeleteImage(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetUserBuilds(ctx context.Context, ownerID string) ([]dto.BuildDTO, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.imageAPI.GetUserBuilds(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return buildsFromProto(resp.GetBuilds()), nil
}

func (c *CoreClient) DeleteBuild(ctx context.Context, ownerID, buildID string) error {
	req := &coreapi.BuildActionRequest{
		OwnerId: ownerID,
		BuildId: buildID,
	}
	_, err := c.imageAPI.DeleteBuild(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetUserProjects(ctx context.Context, ownerID string) ([]dto.ProjectDTO, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.projectAPI.GetUserProjects(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return projectsFromProto(resp.GetProjects()), nil
}

func (c *CoreClient) DeleteProject(ctx context.Context, ownerID, projectID string) error {
	req := &coreapi.ProjectActionRequest{OwnerId: ownerID, ProjectId: projectID}
	_, err := c.projectAPI.DeleteProject(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) StopProject(ctx context.Context, ownerID, projectID string) error {
	req := &coreapi.ProjectActionRequest{OwnerId: ownerID, ProjectId: projectID}
	_, err := c.projectAPI.StopProject(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetAllContainers(ctx context.Context, page, limit int) (dto.PaginatedContainers, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.containerAPI.GetAllContainers(ctx, req)
	if err != nil {
		return dto.PaginatedContainers{}, mapCoreError(err)
	}
	return dto.PaginatedContainers{
		Containers: containersFromProto(resp.GetContainers()),
		TotalCount: resp.GetTotalCount(),
	}, nil
}

func (c *CoreClient) AdminActionContainer(ctx context.Context, containerID, action string) error {
	req := &coreapi.ContainerActionRequest{
		ContainerId: containerID,
		Action:      action,
	}

	_, err := c.containerAPI.AdminActionContainer(ctx, req)

	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetAllVolumes(ctx context.Context, page, limit int) (dto.PaginatedVolumes, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.volumeAPI.GetAllVolumes(ctx, req)
	if err != nil {
		return dto.PaginatedVolumes{}, mapCoreError(err)
	}
	return dto.PaginatedVolumes{
		Volumes:    volumesFromProto(resp.GetVolumes()),
		TotalCount: resp.GetTotalCount(),
	}, nil
}

func (c *CoreClient) AdminDeleteVolume(ctx context.Context, volumeID string) error {
	req := &coreapi.VolumeActionRequest{VolumeId: volumeID}
	_, err := c.volumeAPI.AdminDeleteVolume(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetAllImages(ctx context.Context, page, limit int) (dto.PaginatedImages, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.imageAPI.GetAllImages(ctx, req)
	if err != nil {
		return dto.PaginatedImages{}, mapCoreError(err)
	}
	return dto.PaginatedImages{
		Images:     imagesFromProto(resp.GetImages()),
		TotalCount: resp.GetTotalCount(),
	}, nil
}

func (c *CoreClient) AdminDeleteImage(ctx context.Context, imageID string) error {
	req := &coreapi.ImageActionRequest{ImageId: imageID}
	_, err := c.imageAPI.AdminDeleteImage(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetAllBuilds(ctx context.Context, page, limit int) (dto.PaginatedBuilds, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.imageAPI.GetAllBuilds(ctx, req)
	if err != nil {
		return dto.PaginatedBuilds{}, mapCoreError(err)
	}
	return dto.PaginatedBuilds{
		Builds:     buildsFromProto(resp.GetBuilds()),
		TotalCount: resp.GetTotalCount(),
	}, nil
}

func (c *CoreClient) AdminDeleteBuild(ctx context.Context, buildID string) error {
	req := &coreapi.BuildActionRequest{BuildId: buildID}
	_, err := c.imageAPI.AdminDeleteBuild(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetAllProjects(ctx context.Context, page, limit int) (dto.PaginatedProjects, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.projectAPI.GetAllProjects(ctx, req)
	if err != nil {
		return dto.PaginatedProjects{}, mapCoreError(err)
	}
	return dto.PaginatedProjects{
		Projects:   projectsFromProto(resp.GetProjects()),
		TotalCount: resp.GetTotalCount(),
	}, nil
}

func (c *CoreClient) AdminDeleteProject(ctx context.Context, projectID string) error {
	req := &coreapi.ProjectActionRequest{ProjectId: projectID}
	_, err := c.projectAPI.AdminDeleteProject(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) AdminStopProject(ctx context.Context, projectID string) error {
	req := &coreapi.ProjectActionRequest{ProjectId: projectID}
	_, err := c.projectAPI.AdminStopProject(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetSystemConfig(ctx context.Context) (dto.SystemConfigDTO, error) {
	resp, err := c.systemAPI.GetConfig(ctx, &coreapi.Empty{})
	if err != nil {
		return dto.SystemConfigDTO{}, mapCoreError(err)
	}
	return systemConfigFromProto(resp), nil
}

func (c *CoreClient) UpdateSystemConfig(ctx context.Context, req dto.SystemConfigDTO) error {
	_, err := c.systemAPI.UpdateConfig(ctx, systemConfigToProto(req))
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetContainerStats(ctx context.Context, ownerID, containerID string) (dto.ContainerStatsDTO, error) {
	req := &coreapi.ContainerActionRequest{
		OwnerId:     ownerID,
		ContainerId: containerID,
	}
	resp, err := c.containerAPI.GetContainerStats(ctx, req)
	if err != nil {
		return dto.ContainerStatsDTO{}, mapCoreError(err)
	}
	return containerStatsFromProto(resp), nil
}

func (c *CoreClient) AdminGetContainerStats(ctx context.Context, containerID string) (dto.ContainerStatsDTO, error) {
	req := &coreapi.ContainerActionRequest{
		ContainerId: containerID,
	}
	resp, err := c.containerAPI.AdminGetContainerStats(ctx, req)
	if err != nil {
		return dto.ContainerStatsDTO{}, mapCoreError(err)
	}
	return containerStatsFromProto(resp), nil
}

// TODO: maybe pull out for sso grpc client
func mapCoreError(err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return apperrors.ErrInternal
	}
	switch st.Code() {
	case codes.NotFound:
		return apperrors.ErrNotFound
	case codes.AlreadyExists:
		return apperrors.ErrAlreadyExists
	case codes.ResourceExhausted:
		return apperrors.ErrResourceExhausted
	case codes.InvalidArgument:
		return apperrors.ErrBadRequest
	case codes.Unauthenticated:
		return apperrors.ErrUnauthorized
	default:
		return apperrors.ErrInternal
	}
}

func containersFromProto(items []*coreapi.ContainerData) []dto.ContainerDTO {
	result := make([]dto.ContainerDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.ContainerDTO{
			ID:            item.GetId(),
			DockerID:      item.GetDockerId(),
			Name:          item.GetName(),
			ImageTag:      item.GetImageTag(),
			InternalPort:  item.GetInternalPort(),
			DomainPrefix:  item.GetDomainPrefix(),
			Status:        item.GetStatus(),
			CreatedAt:     item.GetCreatedAt(),
			OwnerID:       item.GetOwnerId(),
			OwnerUsername: item.GetOwnerUsername(),
		})
	}
	return result
}

func volumesFromProto(items []*coreapi.VolumeData) []dto.VolumeDTO {
	result := make([]dto.VolumeDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.VolumeDTO{
			ID:            item.GetId(),
			DockerName:    item.GetDockerName(),
			Driver:        item.GetDriver(),
			CreatedAt:     item.GetCreatedAt(),
			OwnerID:       item.GetOwnerId(),
			OwnerUsername: item.GetOwnerUsername(),
		})
	}
	return result
}

func imagesFromProto(items []*coreapi.ImageData) []dto.ImageDTO {
	result := make([]dto.ImageDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.ImageDTO{
			ID:            item.GetId(),
			Tag:           item.GetTag(),
			SizeMB:        item.GetSizeMb(),
			IsCustom:      item.GetIsCustom(),
			CreatedAt:     item.GetCreatedAt(),
			OwnerID:       item.GetOwnerId(),
			OwnerUsername: item.GetOwnerUsername(),
		})
	}
	return result
}

func buildsFromProto(items []*coreapi.BuildData) []dto.BuildDTO {
	result := make([]dto.BuildDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.BuildDTO{
			ID:            item.GetId(),
			ImageID:       item.GetImageId(),
			Status:        item.GetStatus(),
			StartedAt:     item.GetStartedAt(),
			FinishedAt:    item.GetFinishedAt(),
			LogFilePath:   item.GetLogFilePath(),
			OwnerID:       item.GetOwnerId(),
			OwnerUsername: item.GetOwnerUsername(),
		})
	}
	return result
}

func projectsFromProto(items []*coreapi.ProjectData) []dto.ProjectDTO {
	result := make([]dto.ProjectDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.ProjectDTO{
			ID:            item.GetId(),
			Name:          item.GetName(),
			Status:        item.GetStatus(),
			ErrorMessage:  item.GetErrorMessage(),
			CreatedAt:     item.GetCreatedAt(),
			OwnerID:       item.GetOwnerId(),
			OwnerUsername: item.GetOwnerUsername(),
		})
	}
	return result
}

func systemConfigFromProto(data *coreapi.SystemConfigData) dto.SystemConfigDTO {
	return dto.SystemConfigDTO{
		BaseDomain:                    data.GetBaseDomain(),
		DefaultMemoryReservationBytes: data.GetDefaultMemoryReservationBytes(),
		ReservedSystemMemoryBytes:     data.GetReservedSystemMemoryBytes(),
		OvercommitFactor:              data.GetOvercommitFactor(),
		MaxBurstMultiplier:            data.GetMaxBurstMultiplier(),
		DefaultCpuShares:              data.GetDefaultCpuShares(),
		HighLoadCpuShares:             data.GetHighLoadCpuShares(),
		HighLoadContainerCount:        int(data.GetHighLoadContainerCount()),
		ContainerStopTimeout:          int(data.GetContainerStopTimeout()),
		MaxLogSize:                    data.GetMaxLogSize(),
		MaxLogFiles:                   data.GetMaxLogFiles(),
		ContainerDiskQuota:            data.GetContainerDiskQuota(),
		MaxVolumesPerUser:             int(data.GetMaxVolumesPerUser()),
		MaxContainersPerUser:          int(data.GetMaxContainersPerUser()),
		RegistryApiUrl:                data.GetRegistryApiUrl(),
		RegistryPublicUrl:             data.GetRegistryPublicUrl(),
		ContainerTtlHours:             data.GetContainerTtlHours(),
	}
}

func systemConfigToProto(data dto.SystemConfigDTO) *coreapi.SystemConfigData {
	return &coreapi.SystemConfigData{
		BaseDomain:                    data.BaseDomain,
		DefaultMemoryReservationBytes: data.DefaultMemoryReservationBytes,
		ReservedSystemMemoryBytes:     data.ReservedSystemMemoryBytes,
		OvercommitFactor:              data.OvercommitFactor,
		MaxBurstMultiplier:            data.MaxBurstMultiplier,
		DefaultCpuShares:              data.DefaultCpuShares,
		HighLoadCpuShares:             data.HighLoadCpuShares,
		HighLoadContainerCount:        int32(data.HighLoadContainerCount),
		ContainerStopTimeout:          int32(data.ContainerStopTimeout),
		MaxLogSize:                    data.MaxLogSize,
		MaxLogFiles:                   data.MaxLogFiles,
		ContainerDiskQuota:            data.ContainerDiskQuota,
		MaxVolumesPerUser:             int32(data.MaxVolumesPerUser),
		MaxContainersPerUser:          int32(data.MaxContainersPerUser),
		RegistryApiUrl:                data.RegistryApiUrl,
		RegistryPublicUrl:             data.RegistryPublicUrl,
		ContainerTtlHours:             data.ContainerTtlHours,
	}
}

func containerStatsFromProto(data *coreapi.ContainerStatsResponse) dto.ContainerStatsDTO {
	return dto.ContainerStatsDTO{
		CPUPercentage:    data.GetCpuPercentage(),
		MemoryUsageBytes: data.GetMemoryUsageBytes(),
		MemoryLimitBytes: data.GetMemoryLimitBytes(),
		NetworkRxBytes:   data.GetNetworkRxBytes(),
		NetworkTxBytes:   data.GetNetworkTxBytes(),
	}
}

func userStatsFromProto(data *coreapi.UserStatsResponse) dto.UserStatsDTO {
	return dto.UserStatsDTO{
		ContainersTotal:   data.GetContainersTotal(),
		ContainersRunning: data.GetContainersRunning(),
		ContainersQuota:   data.GetContainersQuota(),
		RamUsedBytes:      data.GetRamUsedBytes(),
		RamQuotaBytes:     data.GetRamQuotaBytes(),
		DiskUsedMB:        data.GetDiskUsedMb(),
		DiskQuotaMB:       data.GetDiskQuotaMb(),
		VolumesTotal:      data.GetVolumesTotal(),
		VolumesQuota:      data.GetVolumesQuota(),
		ImagesTotal:       data.GetImagesTotal(),
		ProjectsTotal:     data.GetProjectsTotal(),
	}
}

func (c *CoreClient) GetUserStats(ctx context.Context, ownerID string) (dto.UserStatsDTO, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.statsAPI.GetUserStats(ctx, req)
	if err != nil {
		return dto.UserStatsDTO{}, mapCoreError(err)
	}
	return userStatsFromProto(resp), nil
}
