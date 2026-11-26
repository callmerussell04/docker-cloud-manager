package handler

import (
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
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

		MemoryReservationMB:      req.MemoryReservationMB,
		CPUReservationMillicores: req.CPUReservationMillicores,
	}
}

func createVolumeInputFromDTO(req dto.CreateVolumeDTO) model.CreateVolumeInput {
	return model.CreateVolumeInput{Name: req.Name, SizeBytes: req.SizeBytes}
}

func containerRefsToDTO(items []model.ContainerRef) []dto.ContainerRefDTO {
	result := make([]dto.ContainerRefDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.ContainerRefDTO{ID: item.ContainerID, Name: item.Name})
	}
	return result
}

func containersToDTO(items []model.Container) []dto.ContainerDTO {
	result := make([]dto.ContainerDTO, 0, len(items))
	for _, item := range items {
		result = append(result, containerToDTO(item))
	}
	return result
}

func containerToDTO(item model.Container) dto.ContainerDTO {
	return dto.ContainerDTO{
		ID:            item.ID,
		DockerID:      item.DockerID,
		ProjectID:     item.ProjectID,
		Name:          item.Name,
		ImageTag:      item.ImageTag,
		InternalPort:  item.InternalPort,
		DomainPrefix:  item.DomainPrefix,
		Status:        item.Status,
		DesiredStatus: item.DesiredStatus,
		LastError:     item.LastError,
		LastExitCode:  item.LastExitCode,
		TTLDeadline:   item.TTLDeadline,
		CreatedAt:     item.CreatedAt,
		OwnerID:       item.OwnerID,
		OwnerUsername: item.OwnerUsername,

		MemoryReservationBytes:   item.MemoryReservationBytes,
		CPUReservationMillicores: item.CPUReservationMillicores,
	}
}

func containersToUserDTO(items []model.Container) []dto.UserContainerDTO {
	result := make([]dto.UserContainerDTO, 0, len(items))
	for _, item := range items {
		result = append(result, containerToUserDTO(item))
	}
	return result
}

func containerToUserDTO(item model.Container) dto.UserContainerDTO {
	return dto.UserContainerDTO{
		ID:            item.ID,
		ProjectID:     item.ProjectID,
		Name:          item.Name,
		ImageTag:      item.ImageTag,
		InternalPort:  item.InternalPort,
		DomainPrefix:  item.DomainPrefix,
		Status:        item.Status,
		DesiredStatus: item.DesiredStatus,
		LastError:     item.LastError,
		LastExitCode:  item.LastExitCode,
		TTLDeadline:   item.TTLDeadline,
		CreatedAt:     item.CreatedAt,

		MemoryReservationBytes:   item.MemoryReservationBytes,
		CPUReservationMillicores: item.CPUReservationMillicores,
	}
}

func containerStatsToDTO(data model.ContainerStats) dto.ContainerStatsDTO {
	return dto.ContainerStatsDTO{
		CPUPercentage:            data.CPUPercentage,
		MemoryUsageBytes:         data.MemoryUsageBytes,
		MemoryLimitBytes:         data.MemoryLimitBytes,
		MemoryStandardLimitBytes: data.MemoryStandardLimitBytes,
		NetworkRxBytes:           data.NetworkRxBytes,
		NetworkTxBytes:           data.NetworkTxBytes,
	}
}

func volumesToDTO(items []model.Volume) []dto.VolumeDTO {
	result := make([]dto.VolumeDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.VolumeDTO{
			ID:              item.ID,
			Name:            item.Name,
			DockerName:      item.DockerName,
			Status:          item.Status,
			LastError:       item.LastError,
			UsedBytes:       item.UsedBytes,
			UsageObservedAt: item.UsageObservedAt,
			SizeBytes:       item.SizeBytes,
			SizeEnforced:    item.SizeEnforced,
			CreatedAt:       item.CreatedAt,
			OwnerID:         item.OwnerID,
			OwnerUsername:   item.OwnerUsername,
			UsedBy:          containerRefsToDTO(item.UsedBy),
		})
	}
	return result
}

func volumesToUserDTO(items []model.Volume) []dto.UserVolumeDTO {
	result := make([]dto.UserVolumeDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.UserVolumeDTO{
			ID:              item.ID,
			Name:            item.Name,
			Status:          item.Status,
			LastError:       item.LastError,
			UsedBytes:       item.UsedBytes,
			UsageObservedAt: item.UsageObservedAt,
			SizeBytes:       item.SizeBytes,
			SizeEnforced:    item.SizeEnforced,
			CreatedAt:       item.CreatedAt,
			UsedBy:          containerRefsToDTO(item.UsedBy),
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
			SizeBytes:     item.SizeBytes,
			Status:        item.Status,
			LastError:     item.LastError,
			CreatedAt:     item.CreatedAt,
			OwnerID:       item.OwnerID,
			OwnerUsername: item.OwnerUsername,
			UsedBy:        containerRefsToDTO(item.UsedBy),
		})
	}
	return result
}

func imagesToUserDTO(items []model.Image) []dto.UserImageDTO {
	result := make([]dto.UserImageDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.UserImageDTO{
			ID:        item.ID,
			Tag:       item.Tag,
			SizeBytes: item.SizeBytes,
			Status:    item.Status,
			LastError: item.LastError,
			CreatedAt: item.CreatedAt,
			UsedBy:    containerRefsToDTO(item.UsedBy),
		})
	}
	return result
}

func buildsToDTO(items []model.Build) []dto.BuildDTO {
	result := make([]dto.BuildDTO, 0, len(items))
	for _, item := range items {
		result = append(result, buildToDTO(item))
	}
	return result
}

func buildToDTO(item model.Build) dto.BuildDTO {
	return dto.BuildDTO{
		ID:                 item.ID,
		ImageID:            item.ImageID,
		Tag:                item.Tag,
		ProjectID:          item.ProjectID,
		ProjectServiceName: item.ProjectServiceName,
		SourceKind:         item.SourceKind,
		SourceLocation:     item.SourceLocation,
		SourceRef:          item.SourceRef,
		Status:             item.Status,
		CreatedAt:          item.CreatedAt,
		StartedAt:          item.StartedAt,
		FinishedAt:         item.FinishedAt,
		LogObjectKey:       item.LogObjectKey,
		OwnerID:            item.OwnerID,
		OwnerUsername:      item.OwnerUsername,
	}
}

func buildsToUserDTO(items []model.Build) []dto.UserBuildDTO {
	result := make([]dto.UserBuildDTO, 0, len(items))
	for _, item := range items {
		result = append(result, buildToUserDTO(item))
	}
	return result
}

func buildToUserDTO(item model.Build) dto.UserBuildDTO {
	return dto.UserBuildDTO{
		ID:                 item.ID,
		ImageID:            item.ImageID,
		Tag:                item.Tag,
		ProjectID:          item.ProjectID,
		ProjectServiceName: item.ProjectServiceName,
		SourceKind:         item.SourceKind,
		SourceLocation:     item.SourceLocation,
		SourceRef:          item.SourceRef,
		Status:             item.Status,
		CreatedAt:          item.CreatedAt,
		StartedAt:          item.StartedAt,
		FinishedAt:         item.FinishedAt,
	}
}

func projectsToDTO(items []model.Project) []dto.ProjectDTO {
	result := make([]dto.ProjectDTO, 0, len(items))
	for _, item := range items {
		result = append(result, projectToDTO(item))
	}
	return result
}

func projectToDTO(item model.Project) dto.ProjectDTO {
	return dto.ProjectDTO{
		ID:            item.ID,
		Name:          item.Name,
		Status:        item.Status,
		ErrorMessage:  item.ErrorMessage,
		LastError:     item.LastError,
		CreatedAt:     item.CreatedAt,
		OwnerID:       item.OwnerID,
		OwnerUsername: item.OwnerUsername,
	}
}

func projectsToUserDTO(items []model.Project) []dto.UserProjectDTO {
	result := make([]dto.UserProjectDTO, 0, len(items))
	for _, item := range items {
		result = append(result, projectToUserDTO(item))
	}
	return result
}

func projectToUserDTO(item model.Project) dto.UserProjectDTO {
	return dto.UserProjectDTO{
		ID:           item.ID,
		Name:         item.Name,
		Status:       item.Status,
		ErrorMessage: item.ErrorMessage,
		LastError:    item.LastError,
		CreatedAt:    item.CreatedAt,
	}
}

func systemConfigFromDTO(data dto.SystemConfigDTO) model.SystemConfig {
	return model.SystemConfig{
		DefaultMemoryReservationBytes:       data.DefaultMemoryReservationBytes,
		ReservedSystemMemoryBytes:           data.ReservedSystemMemoryBytes,
		OvercommitFactor:                    data.OvercommitFactor,
		MaxBurstMultiplier:                  data.MaxBurstMultiplier,
		DefaultCPUReservationMillicores:     data.DefaultCPUReservationMillicores,
		ReservedSystemCPUMillicores:         data.ReservedSystemCPUMillicores,
		CPUOvercommitFactor:                 data.CPUOvercommitFactor,
		MaxCPUBurstMultiplier:               data.MaxCPUBurstMultiplier,
		ContainerStopTimeout:                data.ContainerStopTimeout,
		MaxLogSize:                          data.MaxLogSize,
		ContainerDiskQuota:                  data.ContainerDiskQuota,
		ReservedDomainPrefixes:              data.ReservedDomainPrefixes,
		BlockedDomainPrefixPatterns:         data.BlockedDomainPrefixPatterns,
		MaxVolumesPerUser:                   data.MaxVolumesPerUser,
		MaxContainersPerUser:                data.MaxContainersPerUser,
		ContainerTtlHours:                   data.ContainerTtlHours,
		ContainerPidsLimit:                  data.ContainerPidsLimit,
		ImageBuildsEnabled:                  data.ImageBuildsEnabled,
		BuildMemoryBytes:                    data.BuildMemoryBytes,
		BuildCPUMillicores:                  data.BuildCPUMillicores,
		MaxBuildTimeMinutes:                 data.MaxBuildTimeMinutes,
		MaxConcurrentBuilds:                 data.MaxConcurrentBuilds,
		MaxArchiveSizeBytes:                 data.MaxArchiveSizeBytes,
		MaxUnpackedSizeBytes:                data.MaxUnpackedSizeBytes,
		MaxBuildLogSizeBytes:                data.MaxBuildLogSizeBytes,
		BuildMaxDiskBytes:                   data.BuildMaxDiskBytes,
		MaxStagedSourceBytesPerUser:         data.MaxStagedSourceBytesPerUser,
		MaxQueuedBuildsPerUser:              data.MaxQueuedBuildsPerUser,
		ComposeUploadMaxBytes:               data.ComposeUploadMaxBytes,
		ComposePipelineTimeoutMinutes:       data.ComposePipelineTimeoutMinutes,
		MaxQueuedComposeDeploysPerUser:      data.MaxQueuedComposeDeploysPerUser,
		ComposeDependencyWaitTimeoutMinutes: data.ComposeDependencyWaitTimeoutMinutes,
		HostMinFreeDiskBytes:                data.HostMinFreeDiskBytes,
		MemoryPressureMinFreeBytes:          data.MemoryPressureMinFreeBytes,
		MemoryBoostMultiplier:               data.MemoryBoostMultiplier,
		CPUBoostMultiplier:                  data.CPUBoostMultiplier,
		BoostMinFreeMemoryBytes:             data.BoostMinFreeMemoryBytes,
		DefaultVolumeSizeBytes:              data.DefaultVolumeSizeBytes,
		VolumeSizeEnforcement:               data.VolumeSizeEnforcement,
		DefaultUserQuotaCPU:                 data.DefaultUserQuotaCPU,
		DefaultUserQuotaRAMMB:               data.DefaultUserQuotaRAMMB,
		DefaultUserQuotaDiskMB:              data.DefaultUserQuotaDiskMB,
		GitSourcesEnabled:                   data.GitSourcesEnabled,
		GitAllowedHosts:                     data.GitAllowedHosts,
		GitCloneTimeoutSeconds:              data.GitCloneTimeoutSeconds,
		GitMaxRepositoryBytes:               data.GitMaxRepositoryBytes,
		TelemetryTerminalIdleTimeoutSeconds: data.TelemetryTerminalIdleTimeoutSeconds,
		TelemetryTerminalMaxDurationSeconds: data.TelemetryTerminalMaxDurationSeconds,
		TelemetryAllowedExecCommands:        data.TelemetryAllowedExecCommands,
	}
}

func systemConfigToDTO(data model.SystemConfig) dto.SystemConfigDTO {
	return dto.SystemConfigDTO{
		DefaultMemoryReservationBytes:       data.DefaultMemoryReservationBytes,
		ReservedSystemMemoryBytes:           data.ReservedSystemMemoryBytes,
		OvercommitFactor:                    data.OvercommitFactor,
		MaxBurstMultiplier:                  data.MaxBurstMultiplier,
		DefaultCPUReservationMillicores:     data.DefaultCPUReservationMillicores,
		ReservedSystemCPUMillicores:         data.ReservedSystemCPUMillicores,
		CPUOvercommitFactor:                 data.CPUOvercommitFactor,
		MaxCPUBurstMultiplier:               data.MaxCPUBurstMultiplier,
		ContainerStopTimeout:                data.ContainerStopTimeout,
		MaxLogSize:                          data.MaxLogSize,
		ContainerDiskQuota:                  data.ContainerDiskQuota,
		ReservedDomainPrefixes:              data.ReservedDomainPrefixes,
		BlockedDomainPrefixPatterns:         data.BlockedDomainPrefixPatterns,
		MaxVolumesPerUser:                   data.MaxVolumesPerUser,
		MaxContainersPerUser:                data.MaxContainersPerUser,
		ContainerTtlHours:                   data.ContainerTtlHours,
		ContainerPidsLimit:                  data.ContainerPidsLimit,
		ImageBuildsEnabled:                  data.ImageBuildsEnabled,
		BuildMemoryBytes:                    data.BuildMemoryBytes,
		BuildCPUMillicores:                  data.BuildCPUMillicores,
		MaxBuildTimeMinutes:                 data.MaxBuildTimeMinutes,
		MaxConcurrentBuilds:                 data.MaxConcurrentBuilds,
		MaxArchiveSizeBytes:                 data.MaxArchiveSizeBytes,
		MaxUnpackedSizeBytes:                data.MaxUnpackedSizeBytes,
		MaxBuildLogSizeBytes:                data.MaxBuildLogSizeBytes,
		BuildMaxDiskBytes:                   data.BuildMaxDiskBytes,
		MaxStagedSourceBytesPerUser:         data.MaxStagedSourceBytesPerUser,
		MaxQueuedBuildsPerUser:              data.MaxQueuedBuildsPerUser,
		ComposeUploadMaxBytes:               data.ComposeUploadMaxBytes,
		ComposePipelineTimeoutMinutes:       data.ComposePipelineTimeoutMinutes,
		MaxQueuedComposeDeploysPerUser:      data.MaxQueuedComposeDeploysPerUser,
		ComposeDependencyWaitTimeoutMinutes: data.ComposeDependencyWaitTimeoutMinutes,
		HostMinFreeDiskBytes:                data.HostMinFreeDiskBytes,
		MemoryPressureMinFreeBytes:          data.MemoryPressureMinFreeBytes,
		MemoryBoostMultiplier:               data.MemoryBoostMultiplier,
		CPUBoostMultiplier:                  data.CPUBoostMultiplier,
		BoostMinFreeMemoryBytes:             data.BoostMinFreeMemoryBytes,
		DefaultVolumeSizeBytes:              data.DefaultVolumeSizeBytes,
		VolumeSizeEnforcement:               data.VolumeSizeEnforcement,
		DefaultUserQuotaCPU:                 data.DefaultUserQuotaCPU,
		DefaultUserQuotaRAMMB:               data.DefaultUserQuotaRAMMB,
		DefaultUserQuotaDiskMB:              data.DefaultUserQuotaDiskMB,
		GitSourcesEnabled:                   data.GitSourcesEnabled,
		GitAllowedHosts:                     data.GitAllowedHosts,
		GitCloneTimeoutSeconds:              data.GitCloneTimeoutSeconds,
		GitMaxRepositoryBytes:               data.GitMaxRepositoryBytes,
		TelemetryTerminalIdleTimeoutSeconds: data.TelemetryTerminalIdleTimeoutSeconds,
		TelemetryTerminalMaxDurationSeconds: data.TelemetryTerminalMaxDurationSeconds,
		TelemetryAllowedExecCommands:        data.TelemetryAllowedExecCommands,
	}
}

func userStatsToDTO(data model.UserStats) dto.UserStatsDTO {
	return dto.UserStatsDTO{
		ContainersTotal:    data.ContainersTotal,
		ContainersRunning:  data.ContainersRunning,
		ContainersQuota:    data.ContainersQuota,
		RamUsedBytes:       data.RamUsedBytes,
		RamQuotaBytes:      data.RamQuotaBytes,
		CPUUsedMillicores:  data.CPUUsedMillicores,
		CPUQuotaMillicores: data.CPUQuotaMillicores,
		DiskUsedMB:         data.DiskUsedMB,
		DiskQuotaMB:        data.DiskQuotaMB,
		VolumesTotal:       data.VolumesTotal,
		VolumesQuota:       data.VolumesQuota,
		ImagesTotal:        data.ImagesTotal,
		ProjectsTotal:      data.ProjectsTotal,
	}
}

func systemMonitoringToDTO(data model.SystemMonitoring) dto.SystemMonitoringDTO {
	return dto.SystemMonitoringDTO{
		CPUPercent:                    data.CPUPercent,
		MemoryTotalBytes:              data.MemoryTotalBytes,
		MemoryUsedBytes:               data.MemoryUsedBytes,
		MemoryAvailableBytes:          data.MemoryAvailableBytes,
		DiskTotalBytes:                data.DiskTotalBytes,
		DiskUsedBytes:                 data.DiskUsedBytes,
		DiskFreeBytes:                 data.DiskFreeBytes,
		DCMReservedMemoryBytes:        data.DCMReservedMemoryBytes,
		DCMReservedBuildMemoryBytes:   data.DCMReservedBuildMemoryBytes,
		DCMReservedCPUMillicores:      data.DCMReservedCPUMillicores,
		DCMReservedBuildCPUMillicores: data.DCMReservedBuildCPUMillicores,
		DCMBorrowedMemoryBytes:        data.DCMBorrowedMemoryBytes,
		RootfsQuotaStatus:             data.RootfsQuotaStatus,
		DCMDiskUsedBytes:              data.DCMDiskUsedBytes,
		DCMReservedVolumeBytes:        data.DCMReservedVolumeBytes,
		DCMUnusedVolumePromiseBytes:   data.DCMUnusedVolumePromiseBytes,
		VolumeSizeMode:                data.VolumeSizeMode,
		HostMinFreeDiskBytes:          data.HostMinFreeDiskBytes,
		BuildClaimCap:                 data.BuildClaimCap,
		MaxConcurrentBuilds:           data.MaxConcurrentBuilds,
		BuildsActive:                  data.BuildsActive,
		CPUTotalMillicores:            data.CPUTotalMillicores,
		AdmissionStatus:               data.AdmissionStatus,
		AdmissionReasons:              data.AdmissionReasons,
		ContainersTotal:               data.ContainersTotal,
		ContainersRunning:             data.ContainersRunning,
		ContainersStopped:             data.ContainersStopped,
		ContainersError:               data.ContainersError,
		ContainersMissing:             data.ContainersMissing,
		VolumesTotal:                  data.VolumesTotal,
		ImagesTotal:                   data.ImagesTotal,
		BuildsTotal:                   data.BuildsTotal,
		ProjectsTotal:                 data.ProjectsTotal,
		ObservedAt:                    data.ObservedAt,
	}
}
