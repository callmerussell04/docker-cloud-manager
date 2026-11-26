package client

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/backend/api/core"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/grpcerrors"
)

func (c *CoreClient) GetSystemConfig(ctx context.Context) (model.SystemConfig, error) {
	resp, err := c.systemAPI.GetConfig(ctx, &coreapi.Empty{})
	if err != nil {
		return model.SystemConfig{}, grpcerrors.FromGRPC(err)
	}
	return systemConfigFromProto(resp), nil
}

func (c *CoreClient) UpdateSystemConfig(ctx context.Context, req model.SystemConfig) error {
	_, err := c.systemAPI.UpdateConfig(ctx, systemConfigToProto(req))
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func systemConfigFromProto(data *coreapi.SystemConfigData) model.SystemConfig {
	return model.SystemConfig{
		DefaultMemoryReservationBytes:       data.GetDefaultMemoryReservationBytes(),
		ReservedSystemMemoryBytes:           data.GetReservedSystemMemoryBytes(),
		OvercommitFactor:                    data.GetOvercommitFactor(),
		MaxBurstMultiplier:                  data.GetMaxBurstMultiplier(),
		DefaultCPUReservationMillicores:     data.GetDefaultCpuReservationMillicores(),
		ReservedSystemCPUMillicores:         data.GetReservedSystemCpuMillicores(),
		CPUOvercommitFactor:                 data.GetCpuOvercommitFactor(),
		MaxCPUBurstMultiplier:               data.GetMaxCpuBurstMultiplier(),
		ContainerStopTimeout:                int(data.GetContainerStopTimeout()),
		MaxLogSize:                          data.GetMaxLogSize(),
		ContainerDiskQuota:                  data.GetContainerDiskQuota(),
		ReservedDomainPrefixes:              data.GetReservedDomainPrefixes(),
		BlockedDomainPrefixPatterns:         data.GetBlockedDomainPrefixPatterns(),
		MaxVolumesPerUser:                   int(data.GetMaxVolumesPerUser()),
		MaxContainersPerUser:                int(data.GetMaxContainersPerUser()),
		ContainerTtlHours:                   data.GetContainerTtlHours(),
		ContainerPidsLimit:                  data.GetContainerPidsLimit(),
		ImageBuildsEnabled:                  data.GetImageBuildsEnabled(),
		BuildMemoryBytes:                    data.GetBuildMemoryBytes(),
		BuildCPUMillicores:                  data.GetBuildCpuMillicores(),
		MaxBuildTimeMinutes:                 data.GetMaxBuildTimeMinutes(),
		MaxConcurrentBuilds:                 int(data.GetMaxConcurrentBuilds()),
		MaxArchiveSizeBytes:                 data.GetMaxArchiveSizeBytes(),
		MaxUnpackedSizeBytes:                data.GetMaxUnpackedSizeBytes(),
		MaxBuildLogSizeBytes:                data.GetMaxBuildLogSizeBytes(),
		BuildMaxDiskBytes:                   data.GetBuildMaxDiskBytes(),
		MaxStagedSourceBytesPerUser:         data.GetMaxStagedSourceBytesPerUser(),
		MaxQueuedBuildsPerUser:              int(data.GetMaxQueuedBuildsPerUser()),
		ComposeUploadMaxBytes:               data.GetComposeUploadMaxBytes(),
		ComposePipelineTimeoutMinutes:       data.GetComposePipelineTimeoutMinutes(),
		MaxQueuedComposeDeploysPerUser:      int(data.GetMaxQueuedComposeDeploysPerUser()),
		ComposeDependencyWaitTimeoutMinutes: data.GetComposeDependencyWaitTimeoutMinutes(),
		HostMinFreeDiskBytes:                data.GetHostMinFreeDiskBytes(),
		MemoryPressureMinFreeBytes:          data.GetMemoryPressureMinFreeBytes(),
		MemoryBoostMultiplier:               data.GetMemoryBoostMultiplier(),
		CPUBoostMultiplier:                  data.GetCpuBoostMultiplier(),
		BoostMinFreeMemoryBytes:             data.GetBoostMinFreeMemoryBytes(),
		DefaultVolumeSizeBytes:              data.GetDefaultVolumeSizeBytes(),
		VolumeSizeEnforcement:               data.GetVolumeSizeEnforcement(),
		DefaultUserQuotaCPU:                 data.GetDefaultUserQuotaCpu(),
		DefaultUserQuotaRAMMB:               data.GetDefaultUserQuotaRamMb(),
		DefaultUserQuotaDiskMB:              data.GetDefaultUserQuotaDiskMb(),
		GitSourcesEnabled:                   data.GetGitSourcesEnabled(),
		GitAllowedHosts:                     data.GetGitAllowedHosts(),
		GitCloneTimeoutSeconds:              data.GetGitCloneTimeoutSeconds(),
		GitMaxRepositoryBytes:               data.GetGitMaxRepositoryBytes(),
		TelemetryTerminalIdleTimeoutSeconds: data.GetTelemetryTerminalIdleTimeoutSeconds(),
		TelemetryTerminalMaxDurationSeconds: data.GetTelemetryTerminalMaxDurationSeconds(),
		TelemetryAllowedExecCommands:        data.GetTelemetryAllowedExecCommands(),
	}
}

func systemConfigToProto(data model.SystemConfig) *coreapi.SystemConfigData {
	return &coreapi.SystemConfigData{
		DefaultMemoryReservationBytes:       data.DefaultMemoryReservationBytes,
		ReservedSystemMemoryBytes:           data.ReservedSystemMemoryBytes,
		OvercommitFactor:                    data.OvercommitFactor,
		MaxBurstMultiplier:                  data.MaxBurstMultiplier,
		DefaultCpuReservationMillicores:     data.DefaultCPUReservationMillicores,
		ReservedSystemCpuMillicores:         data.ReservedSystemCPUMillicores,
		CpuOvercommitFactor:                 data.CPUOvercommitFactor,
		MaxCpuBurstMultiplier:               data.MaxCPUBurstMultiplier,
		ContainerStopTimeout:                int32(data.ContainerStopTimeout),
		MaxLogSize:                          data.MaxLogSize,
		ContainerDiskQuota:                  data.ContainerDiskQuota,
		ReservedDomainPrefixes:              data.ReservedDomainPrefixes,
		BlockedDomainPrefixPatterns:         data.BlockedDomainPrefixPatterns,
		MaxVolumesPerUser:                   int32(data.MaxVolumesPerUser),
		MaxContainersPerUser:                int32(data.MaxContainersPerUser),
		ContainerTtlHours:                   data.ContainerTtlHours,
		ContainerPidsLimit:                  data.ContainerPidsLimit,
		ImageBuildsEnabled:                  data.ImageBuildsEnabled,
		BuildMemoryBytes:                    data.BuildMemoryBytes,
		BuildCpuMillicores:                  data.BuildCPUMillicores,
		MaxBuildTimeMinutes:                 data.MaxBuildTimeMinutes,
		MaxConcurrentBuilds:                 int32(data.MaxConcurrentBuilds),
		MaxArchiveSizeBytes:                 data.MaxArchiveSizeBytes,
		MaxUnpackedSizeBytes:                data.MaxUnpackedSizeBytes,
		MaxBuildLogSizeBytes:                data.MaxBuildLogSizeBytes,
		BuildMaxDiskBytes:                   data.BuildMaxDiskBytes,
		MaxStagedSourceBytesPerUser:         data.MaxStagedSourceBytesPerUser,
		MaxQueuedBuildsPerUser:              int32(data.MaxQueuedBuildsPerUser),
		ComposeUploadMaxBytes:               data.ComposeUploadMaxBytes,
		ComposePipelineTimeoutMinutes:       data.ComposePipelineTimeoutMinutes,
		MaxQueuedComposeDeploysPerUser:      int32(data.MaxQueuedComposeDeploysPerUser),
		ComposeDependencyWaitTimeoutMinutes: data.ComposeDependencyWaitTimeoutMinutes,
		HostMinFreeDiskBytes:                data.HostMinFreeDiskBytes,
		MemoryPressureMinFreeBytes:          data.MemoryPressureMinFreeBytes,
		MemoryBoostMultiplier:               data.MemoryBoostMultiplier,
		CpuBoostMultiplier:                  data.CPUBoostMultiplier,
		BoostMinFreeMemoryBytes:             data.BoostMinFreeMemoryBytes,
		DefaultVolumeSizeBytes:              data.DefaultVolumeSizeBytes,
		VolumeSizeEnforcement:               data.VolumeSizeEnforcement,
		DefaultUserQuotaCpu:                 data.DefaultUserQuotaCPU,
		DefaultUserQuotaRamMb:               data.DefaultUserQuotaRAMMB,
		DefaultUserQuotaDiskMb:              data.DefaultUserQuotaDiskMB,
		GitSourcesEnabled:                   data.GitSourcesEnabled,
		GitAllowedHosts:                     data.GitAllowedHosts,
		GitCloneTimeoutSeconds:              data.GitCloneTimeoutSeconds,
		GitMaxRepositoryBytes:               data.GitMaxRepositoryBytes,
		TelemetryTerminalIdleTimeoutSeconds: data.TelemetryTerminalIdleTimeoutSeconds,
		TelemetryTerminalMaxDurationSeconds: data.TelemetryTerminalMaxDurationSeconds,
		TelemetryAllowedExecCommands:        data.TelemetryAllowedExecCommands,
	}
}
