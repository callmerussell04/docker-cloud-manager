package handler

import (
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
)

func createContainerInputFromDTO(req dto.CreateContainerDTO) model.CreateContainerInput {
	mounts := make([]model.VolumeMountInput, 0, len(req.VolumeMounts))
	for _, m := range req.VolumeMounts {
		mounts = append(mounts, model.VolumeMountInput{
			VolumeID:   m.VolumeID,
			MountPath:  m.MountPath,
			IsReadOnly: m.IsReadOnly,
		})
	}
	return model.CreateContainerInput{
		Name:         req.Name,
		ImageTag:     req.ImageTag,
		InternalPort: req.InternalPort,
		EnvVars:      req.EnvVars,
		VolumeMounts: mounts,
		DomainPrefix: req.DomainPrefix,
	}
}

func createVolumeInputFromDTO(req dto.CreateVolumeDTO) model.CreateVolumeInput {
	return model.CreateVolumeInput{Name: req.Name}
}

func containersToDTO(items []model.Container) []dto.ContainerDTO {
	result := make([]dto.ContainerDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.ContainerDTO{
			ID:            item.ID,
			DockerID:      item.DockerID,
			Name:          item.Name,
			ImageTag:      item.ImageTag,
			InternalPort:  item.InternalPort,
			DomainPrefix:  item.DomainPrefix,
			Status:        item.Status,
			CreatedAt:     item.CreatedAt,
			OwnerID:       item.OwnerID,
			OwnerUsername: item.OwnerUsername,
		})
	}
	return result
}

func containerStatsToDTO(data model.ContainerStats) dto.ContainerStatsDTO {
	return dto.ContainerStatsDTO{
		CPUPercentage:    data.CPUPercentage,
		MemoryUsageBytes: data.MemoryUsageBytes,
		MemoryLimitBytes: data.MemoryLimitBytes,
		NetworkRxBytes:   data.NetworkRxBytes,
		NetworkTxBytes:   data.NetworkTxBytes,
	}
}

func volumesToDTO(items []model.Volume) []dto.VolumeDTO {
	result := make([]dto.VolumeDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.VolumeDTO{
			ID:            item.ID,
			DockerName:    item.DockerName,
			Driver:        item.Driver,
			CreatedAt:     item.CreatedAt,
			OwnerID:       item.OwnerID,
			OwnerUsername: item.OwnerUsername,
		})
	}
	return result
}

func imagesToDTO(items []model.Image) []dto.ImageDTO {
	result := make([]dto.ImageDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.ImageDTO{
			ID:            item.ID,
			Tag:           item.Tag,
			SizeMB:        item.SizeMB,
			IsCustom:      item.IsCustom,
			CreatedAt:     item.CreatedAt,
			OwnerID:       item.OwnerID,
			OwnerUsername: item.OwnerUsername,
		})
	}
	return result
}

func buildsToDTO(items []model.Build) []dto.BuildDTO {
	result := make([]dto.BuildDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.BuildDTO{
			ID:            item.ID,
			ImageID:       item.ImageID,
			Status:        item.Status,
			StartedAt:     item.StartedAt,
			FinishedAt:    item.FinishedAt,
			LogFilePath:   item.LogFilePath,
			OwnerID:       item.OwnerID,
			OwnerUsername: item.OwnerUsername,
		})
	}
	return result
}

func projectsToDTO(items []model.Project) []dto.ProjectDTO {
	result := make([]dto.ProjectDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.ProjectDTO{
			ID:            item.ID,
			Name:          item.Name,
			Status:        item.Status,
			ErrorMessage:  item.ErrorMessage,
			CreatedAt:     item.CreatedAt,
			OwnerID:       item.OwnerID,
			OwnerUsername: item.OwnerUsername,
		})
	}
	return result
}

func systemConfigFromDTO(data dto.SystemConfigDTO) model.SystemConfig {
	return model.SystemConfig{
		BaseDomain:                    data.BaseDomain,
		DefaultMemoryReservationBytes: data.DefaultMemoryReservationBytes,
		ReservedSystemMemoryBytes:     data.ReservedSystemMemoryBytes,
		OvercommitFactor:              data.OvercommitFactor,
		MaxBurstMultiplier:            data.MaxBurstMultiplier,
		DefaultCpuShares:              data.DefaultCpuShares,
		HighLoadCpuShares:             data.HighLoadCpuShares,
		HighLoadContainerCount:        data.HighLoadContainerCount,
		ContainerStopTimeout:          data.ContainerStopTimeout,
		MaxLogSize:                    data.MaxLogSize,
		MaxLogFiles:                   data.MaxLogFiles,
		ContainerDiskQuota:            data.ContainerDiskQuota,
		MaxVolumesPerUser:             data.MaxVolumesPerUser,
		MaxContainersPerUser:          data.MaxContainersPerUser,
		RegistryApiUrl:                data.RegistryApiUrl,
		RegistryPublicUrl:             data.RegistryPublicUrl,
		ContainerTtlHours:             data.ContainerTtlHours,
	}
}

func systemConfigToDTO(data model.SystemConfig) dto.SystemConfigDTO {
	return dto.SystemConfigDTO{
		BaseDomain:                    data.BaseDomain,
		DefaultMemoryReservationBytes: data.DefaultMemoryReservationBytes,
		ReservedSystemMemoryBytes:     data.ReservedSystemMemoryBytes,
		OvercommitFactor:              data.OvercommitFactor,
		MaxBurstMultiplier:            data.MaxBurstMultiplier,
		DefaultCpuShares:              data.DefaultCpuShares,
		HighLoadCpuShares:             data.HighLoadCpuShares,
		HighLoadContainerCount:        data.HighLoadContainerCount,
		ContainerStopTimeout:          data.ContainerStopTimeout,
		MaxLogSize:                    data.MaxLogSize,
		MaxLogFiles:                   data.MaxLogFiles,
		ContainerDiskQuota:            data.ContainerDiskQuota,
		MaxVolumesPerUser:             data.MaxVolumesPerUser,
		MaxContainersPerUser:          data.MaxContainersPerUser,
		RegistryApiUrl:                data.RegistryApiUrl,
		RegistryPublicUrl:             data.RegistryPublicUrl,
		ContainerTtlHours:             data.ContainerTtlHours,
	}
}

func userStatsToDTO(data model.UserStats) dto.UserStatsDTO {
	return dto.UserStatsDTO{
		ContainersTotal:   data.ContainersTotal,
		ContainersRunning: data.ContainersRunning,
		ContainersQuota:   data.ContainersQuota,
		RamUsedBytes:      data.RamUsedBytes,
		RamQuotaBytes:     data.RamQuotaBytes,
		DiskUsedMB:        data.DiskUsedMB,
		DiskQuotaMB:       data.DiskQuotaMB,
		VolumesTotal:      data.VolumesTotal,
		VolumesQuota:      data.VolumesQuota,
		ImagesTotal:       data.ImagesTotal,
		ProjectsTotal:     data.ProjectsTotal,
	}
}
