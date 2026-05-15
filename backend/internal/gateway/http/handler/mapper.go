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
			DesiredStatus: item.DesiredStatus,
			LastError:     item.LastError,
			LastExitCode:  item.LastExitCode,
			CreatedAt:     item.CreatedAt,
			OwnerID:       item.OwnerID,
			OwnerUsername: item.OwnerUsername,
		})
	}
	return result
}

func containersToUserDTO(items []model.Container) []dto.UserContainerDTO {
	result := make([]dto.UserContainerDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.UserContainerDTO{
			ID:            item.ID,
			Name:          item.Name,
			ImageTag:      item.ImageTag,
			InternalPort:  item.InternalPort,
			DomainPrefix:  item.DomainPrefix,
			Status:        item.Status,
			DesiredStatus: item.DesiredStatus,
			LastError:     item.LastError,
			LastExitCode:  item.LastExitCode,
			CreatedAt:     item.CreatedAt,
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
			ID:              item.ID,
			DockerName:      item.DockerName,
			Status:          item.Status,
			LastError:       item.LastError,
			UsedBytes:       item.UsedBytes,
			UsageObservedAt: item.UsageObservedAt,
			CreatedAt:       item.CreatedAt,
			OwnerID:         item.OwnerID,
			OwnerUsername:   item.OwnerUsername,
		})
	}
	return result
}

func volumesToUserDTO(items []model.Volume) []dto.UserVolumeDTO {
	result := make([]dto.UserVolumeDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.UserVolumeDTO{
			ID:              item.ID,
			Status:          item.Status,
			LastError:       item.LastError,
			UsedBytes:       item.UsedBytes,
			UsageObservedAt: item.UsageObservedAt,
			CreatedAt:       item.CreatedAt,
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
			Status:        item.Status,
			LastError:     item.LastError,
			CreatedAt:     item.CreatedAt,
			OwnerID:       item.OwnerID,
			OwnerUsername: item.OwnerUsername,
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
			SizeMB:    item.SizeMB,
			Status:    item.Status,
			LastError: item.LastError,
			CreatedAt: item.CreatedAt,
		})
	}
	return result
}

func buildsToDTO(items []model.Build) []dto.BuildDTO {
	result := make([]dto.BuildDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.BuildDTO{
			ID:                 item.ID,
			ImageID:            item.ImageID,
			ProjectID:          item.ProjectID,
			ProjectServiceName: item.ProjectServiceName,
			Status:             item.Status,
			StartedAt:          item.StartedAt,
			FinishedAt:         item.FinishedAt,
			LogFilePath:        item.LogFilePath,
			OwnerID:            item.OwnerID,
			OwnerUsername:      item.OwnerUsername,
		})
	}
	return result
}

func buildsToUserDTO(items []model.Build) []dto.UserBuildDTO {
	result := make([]dto.UserBuildDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.UserBuildDTO{
			ID:                 item.ID,
			ImageID:            item.ImageID,
			ProjectID:          item.ProjectID,
			ProjectServiceName: item.ProjectServiceName,
			Status:             item.Status,
			StartedAt:          item.StartedAt,
			FinishedAt:         item.FinishedAt,
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
			LastError:     item.LastError,
			CreatedAt:     item.CreatedAt,
			OwnerID:       item.OwnerID,
			OwnerUsername: item.OwnerUsername,
		})
	}
	return result
}

func projectsToUserDTO(items []model.Project) []dto.UserProjectDTO {
	result := make([]dto.UserProjectDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.UserProjectDTO{
			ID:           item.ID,
			Name:         item.Name,
			Status:       item.Status,
			ErrorMessage: item.ErrorMessage,
			LastError:    item.LastError,
			CreatedAt:    item.CreatedAt,
		})
	}
	return result
}

func systemConfigFromDTO(data dto.SystemConfigDTO) model.SystemConfig {
	return model.SystemConfig{
		BaseDomain:                           data.BaseDomain,
		DefaultMemoryReservationBytes:        data.DefaultMemoryReservationBytes,
		ReservedSystemMemoryBytes:            data.ReservedSystemMemoryBytes,
		OvercommitFactor:                     data.OvercommitFactor,
		MaxBurstMultiplier:                   data.MaxBurstMultiplier,
		DefaultCpuShares:                     data.DefaultCpuShares,
		HighLoadCpuShares:                    data.HighLoadCpuShares,
		HighLoadContainerCount:               data.HighLoadContainerCount,
		ContainerStopTimeout:                 data.ContainerStopTimeout,
		MaxLogSize:                           data.MaxLogSize,
		MaxLogFiles:                          data.MaxLogFiles,
		ContainerDiskQuota:                   data.ContainerDiskQuota,
		ReservedDomainPrefixes:               data.ReservedDomainPrefixes,
		MaxVolumesPerUser:                    data.MaxVolumesPerUser,
		MaxContainersPerUser:                 data.MaxContainersPerUser,
		RegistryApiUrl:                       data.RegistryApiUrl,
		RegistryPublicUrl:                    data.RegistryPublicUrl,
		ContainerTtlHours:                    data.ContainerTtlHours,
		ContainerPidsLimit:                   data.ContainerPidsLimit,
		ContainerMemorySwapMultiplier:        data.ContainerMemorySwapMultiplier,
		ProxyNetworkName:                     data.ProxyNetworkName,
		RegistryContainerName:                data.RegistryContainerName,
		ImageBuildsEnabled:                   data.ImageBuildsEnabled,
		BuildMemoryBytes:                     data.BuildMemoryBytes,
		BuildCpuQuota:                        data.BuildCpuQuota,
		BuildCpuPeriod:                       data.BuildCpuPeriod,
		BuildMemorySwapMultiplier:            data.BuildMemorySwapMultiplier,
		BuildPidsLimit:                       data.BuildPidsLimit,
		BuildNetworkName:                     data.BuildNetworkName,
		KanikoImage:                          data.KanikoImage,
		MaxBuildTimeMinutes:                  data.MaxBuildTimeMinutes,
		MaxConcurrentBuilds:                  data.MaxConcurrentBuilds,
		MaxUploadSizeBytes:                   data.MaxUploadSizeBytes,
		MaxArchiveSizeBytes:                  data.MaxArchiveSizeBytes,
		MaxUnpackedSizeBytes:                 data.MaxUnpackedSizeBytes,
		MaxBuildLogSizeBytes:                 data.MaxBuildLogSizeBytes,
		BuildCancelPollIntervalSeconds:       data.BuildCancelPollIntervalSeconds,
		TtlWorkerIntervalSeconds:             data.TtlWorkerIntervalSeconds,
		GcWorkerIntervalMinutes:              data.GcWorkerIntervalMinutes,
		StaleBuildTimeoutMinutes:             data.StaleBuildTimeoutMinutes,
		EventSyncIntervalSeconds:             data.EventSyncIntervalSeconds,
		EventReconnectDelaySeconds:           data.EventReconnectDelaySeconds,
		BuildOutboxIntervalSeconds:           data.BuildOutboxIntervalSeconds,
		BuildOutboxBatchSize:                 data.BuildOutboxBatchSize,
		ReportsUsageSnapshotIntervalSeconds:  data.ReportsUsageSnapshotIntervalSeconds,
		ContainerCreateWorkerCount:           data.ContainerCreateWorkerCount,
		ContainerCreateMaxAttempts:           data.ContainerCreateMaxAttempts,
		ContainerCreateTimeoutMinutes:        data.ContainerCreateTimeoutMinutes,
		MaxQueuedContainerCreatesPerUser:     data.MaxQueuedContainerCreatesPerUser,
		ContainerCreateOutboxIntervalSeconds: data.ContainerCreateOutboxIntervalSeconds,
		ContainerCreateOutboxBatchSize:       data.ContainerCreateOutboxBatchSize,
		MaxStagedSourceBytesPerUser:          data.MaxStagedSourceBytesPerUser,
		MaxQueuedBuildsPerUser:               data.MaxQueuedBuildsPerUser,
		ComposeUploadMaxBytes:                data.ComposeUploadMaxBytes,
		ComposePipelineTimeoutMinutes:        data.ComposePipelineTimeoutMinutes,
		ComposeDeployWorkerCount:             data.ComposeDeployWorkerCount,
		ComposeOutboxIntervalSeconds:         data.ComposeOutboxIntervalSeconds,
		ComposeOutboxBatchSize:               data.ComposeOutboxBatchSize,
		ComposeDeployMaxAttempts:             data.ComposeDeployMaxAttempts,
		MaxQueuedComposeDeploysPerUser:       data.MaxQueuedComposeDeploysPerUser,
		ComposeBuildPollIntervalSeconds:      data.ComposeBuildPollIntervalSeconds,
		ComposeDependencyWaitTimeoutMinutes:  data.ComposeDependencyWaitTimeoutMinutes,
		ComposeDependencyPollIntervalSeconds: data.ComposeDependencyPollIntervalSeconds,
		ComposeCoordinatorIntervalSeconds:    data.ComposeCoordinatorIntervalSeconds,
		HostMinFreeDiskBytes:                 data.HostMinFreeDiskBytes,
		GitSourcesEnabled:                    data.GitSourcesEnabled,
		GitAllowedHosts:                      data.GitAllowedHosts,
		GitCloneTimeoutSeconds:               data.GitCloneTimeoutSeconds,
		GitMaxRepositoryBytes:                data.GitMaxRepositoryBytes,
		TelemetryMaxLogTailLines:             data.TelemetryMaxLogTailLines,
		TelemetryMaxLogStreamsPerUser:        data.TelemetryMaxLogStreamsPerUser,
		TelemetryMaxTerminalSessionsPerUser:  data.TelemetryMaxTerminalSessionsPerUser,
		TelemetryTerminalIdleTimeoutSeconds:  data.TelemetryTerminalIdleTimeoutSeconds,
		TelemetryTerminalMaxDurationSeconds:  data.TelemetryTerminalMaxDurationSeconds,
		TelemetryAllowedExecCommands:         data.TelemetryAllowedExecCommands,
		TelemetryMaxCommandArgs:              data.TelemetryMaxCommandArgs,
		TelemetryMaxCommandArgBytes:          data.TelemetryMaxCommandArgBytes,
		TelemetryWSReadLimitBytes:            data.TelemetryWSReadLimitBytes,
	}
}

func systemConfigToDTO(data model.SystemConfig) dto.SystemConfigDTO {
	return dto.SystemConfigDTO{
		BaseDomain:                           data.BaseDomain,
		DefaultMemoryReservationBytes:        data.DefaultMemoryReservationBytes,
		ReservedSystemMemoryBytes:            data.ReservedSystemMemoryBytes,
		OvercommitFactor:                     data.OvercommitFactor,
		MaxBurstMultiplier:                   data.MaxBurstMultiplier,
		DefaultCpuShares:                     data.DefaultCpuShares,
		HighLoadCpuShares:                    data.HighLoadCpuShares,
		HighLoadContainerCount:               data.HighLoadContainerCount,
		ContainerStopTimeout:                 data.ContainerStopTimeout,
		MaxLogSize:                           data.MaxLogSize,
		MaxLogFiles:                          data.MaxLogFiles,
		ContainerDiskQuota:                   data.ContainerDiskQuota,
		ReservedDomainPrefixes:               data.ReservedDomainPrefixes,
		MaxVolumesPerUser:                    data.MaxVolumesPerUser,
		MaxContainersPerUser:                 data.MaxContainersPerUser,
		RegistryApiUrl:                       data.RegistryApiUrl,
		RegistryPublicUrl:                    data.RegistryPublicUrl,
		ContainerTtlHours:                    data.ContainerTtlHours,
		ContainerPidsLimit:                   data.ContainerPidsLimit,
		ContainerMemorySwapMultiplier:        data.ContainerMemorySwapMultiplier,
		ProxyNetworkName:                     data.ProxyNetworkName,
		RegistryContainerName:                data.RegistryContainerName,
		ImageBuildsEnabled:                   data.ImageBuildsEnabled,
		BuildMemoryBytes:                     data.BuildMemoryBytes,
		BuildCpuQuota:                        data.BuildCpuQuota,
		BuildCpuPeriod:                       data.BuildCpuPeriod,
		BuildMemorySwapMultiplier:            data.BuildMemorySwapMultiplier,
		BuildPidsLimit:                       data.BuildPidsLimit,
		BuildNetworkName:                     data.BuildNetworkName,
		KanikoImage:                          data.KanikoImage,
		MaxBuildTimeMinutes:                  data.MaxBuildTimeMinutes,
		MaxConcurrentBuilds:                  data.MaxConcurrentBuilds,
		MaxUploadSizeBytes:                   data.MaxUploadSizeBytes,
		MaxArchiveSizeBytes:                  data.MaxArchiveSizeBytes,
		MaxUnpackedSizeBytes:                 data.MaxUnpackedSizeBytes,
		MaxBuildLogSizeBytes:                 data.MaxBuildLogSizeBytes,
		BuildCancelPollIntervalSeconds:       data.BuildCancelPollIntervalSeconds,
		TtlWorkerIntervalSeconds:             data.TtlWorkerIntervalSeconds,
		GcWorkerIntervalMinutes:              data.GcWorkerIntervalMinutes,
		StaleBuildTimeoutMinutes:             data.StaleBuildTimeoutMinutes,
		EventSyncIntervalSeconds:             data.EventSyncIntervalSeconds,
		EventReconnectDelaySeconds:           data.EventReconnectDelaySeconds,
		BuildOutboxIntervalSeconds:           data.BuildOutboxIntervalSeconds,
		BuildOutboxBatchSize:                 data.BuildOutboxBatchSize,
		ReportsUsageSnapshotIntervalSeconds:  data.ReportsUsageSnapshotIntervalSeconds,
		ContainerCreateWorkerCount:           data.ContainerCreateWorkerCount,
		ContainerCreateMaxAttempts:           data.ContainerCreateMaxAttempts,
		ContainerCreateTimeoutMinutes:        data.ContainerCreateTimeoutMinutes,
		MaxQueuedContainerCreatesPerUser:     data.MaxQueuedContainerCreatesPerUser,
		ContainerCreateOutboxIntervalSeconds: data.ContainerCreateOutboxIntervalSeconds,
		ContainerCreateOutboxBatchSize:       data.ContainerCreateOutboxBatchSize,
		MaxStagedSourceBytesPerUser:          data.MaxStagedSourceBytesPerUser,
		MaxQueuedBuildsPerUser:               data.MaxQueuedBuildsPerUser,
		ComposeUploadMaxBytes:                data.ComposeUploadMaxBytes,
		ComposePipelineTimeoutMinutes:        data.ComposePipelineTimeoutMinutes,
		ComposeDeployWorkerCount:             data.ComposeDeployWorkerCount,
		ComposeOutboxIntervalSeconds:         data.ComposeOutboxIntervalSeconds,
		ComposeOutboxBatchSize:               data.ComposeOutboxBatchSize,
		ComposeDeployMaxAttempts:             data.ComposeDeployMaxAttempts,
		MaxQueuedComposeDeploysPerUser:       data.MaxQueuedComposeDeploysPerUser,
		ComposeBuildPollIntervalSeconds:      data.ComposeBuildPollIntervalSeconds,
		ComposeDependencyWaitTimeoutMinutes:  data.ComposeDependencyWaitTimeoutMinutes,
		ComposeDependencyPollIntervalSeconds: data.ComposeDependencyPollIntervalSeconds,
		ComposeCoordinatorIntervalSeconds:    data.ComposeCoordinatorIntervalSeconds,
		HostMinFreeDiskBytes:                 data.HostMinFreeDiskBytes,
		GitSourcesEnabled:                    data.GitSourcesEnabled,
		GitAllowedHosts:                      data.GitAllowedHosts,
		GitCloneTimeoutSeconds:               data.GitCloneTimeoutSeconds,
		GitMaxRepositoryBytes:                data.GitMaxRepositoryBytes,
		TelemetryMaxLogTailLines:             data.TelemetryMaxLogTailLines,
		TelemetryMaxLogStreamsPerUser:        data.TelemetryMaxLogStreamsPerUser,
		TelemetryMaxTerminalSessionsPerUser:  data.TelemetryMaxTerminalSessionsPerUser,
		TelemetryTerminalIdleTimeoutSeconds:  data.TelemetryTerminalIdleTimeoutSeconds,
		TelemetryTerminalMaxDurationSeconds:  data.TelemetryTerminalMaxDurationSeconds,
		TelemetryAllowedExecCommands:         data.TelemetryAllowedExecCommands,
		TelemetryMaxCommandArgs:              data.TelemetryMaxCommandArgs,
		TelemetryMaxCommandArgBytes:          data.TelemetryMaxCommandArgBytes,
		TelemetryWSReadLimitBytes:            data.TelemetryWSReadLimitBytes,
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

func systemMonitoringToDTO(data model.SystemMonitoring) dto.SystemMonitoringDTO {
	return dto.SystemMonitoringDTO{
		CPUPercent:             data.CPUPercent,
		MemoryTotalBytes:       data.MemoryTotalBytes,
		MemoryUsedBytes:        data.MemoryUsedBytes,
		MemoryAvailableBytes:   data.MemoryAvailableBytes,
		DiskTotalBytes:         data.DiskTotalBytes,
		DiskUsedBytes:          data.DiskUsedBytes,
		DiskFreeBytes:          data.DiskFreeBytes,
		DCMReservedMemoryBytes: data.DCMReservedMemoryBytes,
		DCMDiskUsedBytes:       data.DCMDiskUsedBytes,
		ContainersTotal:        data.ContainersTotal,
		ContainersRunning:      data.ContainersRunning,
		ContainersStopped:      data.ContainersStopped,
		ContainersError:        data.ContainersError,
		ContainersMissing:      data.ContainersMissing,
		VolumesTotal:           data.VolumesTotal,
		ImagesTotal:            data.ImagesTotal,
		BuildsTotal:            data.BuildsTotal,
		ProjectsTotal:          data.ProjectsTotal,
		ObservedAt:             data.ObservedAt,
	}
}
