package grpcclient

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
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
		BaseDomain:                           data.GetBaseDomain(),
		DefaultMemoryReservationBytes:        data.GetDefaultMemoryReservationBytes(),
		ReservedSystemMemoryBytes:            data.GetReservedSystemMemoryBytes(),
		OvercommitFactor:                     data.GetOvercommitFactor(),
		MaxBurstMultiplier:                   data.GetMaxBurstMultiplier(),
		DefaultCpuShares:                     data.GetDefaultCpuShares(),
		HighLoadCpuShares:                    data.GetHighLoadCpuShares(),
		HighLoadContainerCount:               int(data.GetHighLoadContainerCount()),
		ContainerStopTimeout:                 int(data.GetContainerStopTimeout()),
		MaxLogSize:                           data.GetMaxLogSize(),
		MaxLogFiles:                          data.GetMaxLogFiles(),
		ContainerDiskQuota:                   data.GetContainerDiskQuota(),
		ReservedDomainPrefixes:               data.GetReservedDomainPrefixes(),
		MaxVolumesPerUser:                    int(data.GetMaxVolumesPerUser()),
		MaxContainersPerUser:                 int(data.GetMaxContainersPerUser()),
		RegistryApiUrl:                       data.GetRegistryApiUrl(),
		RegistryPublicUrl:                    data.GetRegistryPublicUrl(),
		ContainerTtlHours:                    data.GetContainerTtlHours(),
		ContainerPidsLimit:                   data.GetContainerPidsLimit(),
		ContainerMemorySwapMultiplier:        data.GetContainerMemorySwapMultiplier(),
		ProxyNetworkName:                     data.GetProxyNetworkName(),
		RegistryContainerName:                data.GetRegistryContainerName(),
		ImageBuildsEnabled:                   data.GetImageBuildsEnabled(),
		BuildMemoryBytes:                     data.GetBuildMemoryBytes(),
		BuildCpuQuota:                        data.GetBuildCpuQuota(),
		BuildCpuPeriod:                       data.GetBuildCpuPeriod(),
		BuildMemorySwapMultiplier:            data.GetBuildMemorySwapMultiplier(),
		BuildPidsLimit:                       data.GetBuildPidsLimit(),
		BuildNetworkName:                     data.GetBuildNetworkName(),
		KanikoImage:                          data.GetKanikoImage(),
		MaxBuildTimeMinutes:                  data.GetMaxBuildTimeMinutes(),
		MaxConcurrentBuilds:                  int(data.GetMaxConcurrentBuilds()),
		MaxUploadSizeBytes:                   data.GetMaxUploadSizeBytes(),
		MaxArchiveSizeBytes:                  data.GetMaxArchiveSizeBytes(),
		MaxUnpackedSizeBytes:                 data.GetMaxUnpackedSizeBytes(),
		MaxBuildLogSizeBytes:                 data.GetMaxBuildLogSizeBytes(),
		BuildCancelPollIntervalSeconds:       data.GetBuildCancelPollIntervalSeconds(),
		TtlWorkerIntervalSeconds:             data.GetTtlWorkerIntervalSeconds(),
		GcWorkerIntervalMinutes:              data.GetGcWorkerIntervalMinutes(),
		StaleBuildTimeoutMinutes:             data.GetStaleBuildTimeoutMinutes(),
		EventSyncIntervalSeconds:             data.GetEventSyncIntervalSeconds(),
		EventReconnectDelaySeconds:           data.GetEventReconnectDelaySeconds(),
		BuildOutboxIntervalSeconds:           data.GetBuildOutboxIntervalSeconds(),
		BuildOutboxBatchSize:                 int(data.GetBuildOutboxBatchSize()),
		ComposeUploadMaxBytes:                data.GetComposeUploadMaxBytes(),
		ComposePipelineTimeoutMinutes:        data.GetComposePipelineTimeoutMinutes(),
		ComposeDeployWorkerCount:             int(data.GetComposeDeployWorkerCount()),
		ComposeOutboxIntervalSeconds:         data.GetComposeOutboxIntervalSeconds(),
		ComposeOutboxBatchSize:               int(data.GetComposeOutboxBatchSize()),
		ComposeDeployMaxAttempts:             int(data.GetComposeDeployMaxAttempts()),
		ComposeBuildPollIntervalSeconds:      data.GetComposeBuildPollIntervalSeconds(),
		ComposeDependencyWaitTimeoutMinutes:  data.GetComposeDependencyWaitTimeoutMinutes(),
		ComposeDependencyPollIntervalSeconds: data.GetComposeDependencyPollIntervalSeconds(),
		GitSourcesEnabled:                    data.GetGitSourcesEnabled(),
		GitAllowedHosts:                      data.GetGitAllowedHosts(),
		GitCloneTimeoutSeconds:               data.GetGitCloneTimeoutSeconds(),
		GitMaxRepositoryBytes:                data.GetGitMaxRepositoryBytes(),
		TelemetryMaxLogTailLines:             int(data.GetTelemetryMaxLogTailLines()),
		TelemetryMaxLogStreamsPerUser:        int(data.GetTelemetryMaxLogStreamsPerUser()),
		TelemetryMaxTerminalSessionsPerUser:  int(data.GetTelemetryMaxTerminalSessionsPerUser()),
		TelemetryTerminalIdleTimeoutSeconds:  data.GetTelemetryTerminalIdleTimeoutSeconds(),
		TelemetryTerminalMaxDurationSeconds:  data.GetTelemetryTerminalMaxDurationSeconds(),
		TelemetryAllowedExecCommands:         data.GetTelemetryAllowedExecCommands(),
		TelemetryMaxCommandArgs:              int(data.GetTelemetryMaxCommandArgs()),
		TelemetryMaxCommandArgBytes:          int(data.GetTelemetryMaxCommandArgBytes()),
		TelemetryWSReadLimitBytes:            data.GetTelemetryWsReadLimitBytes(),
	}
}

func systemConfigToProto(data model.SystemConfig) *coreapi.SystemConfigData {
	return &coreapi.SystemConfigData{
		BaseDomain:                           data.BaseDomain,
		DefaultMemoryReservationBytes:        data.DefaultMemoryReservationBytes,
		ReservedSystemMemoryBytes:            data.ReservedSystemMemoryBytes,
		OvercommitFactor:                     data.OvercommitFactor,
		MaxBurstMultiplier:                   data.MaxBurstMultiplier,
		DefaultCpuShares:                     data.DefaultCpuShares,
		HighLoadCpuShares:                    data.HighLoadCpuShares,
		HighLoadContainerCount:               int32(data.HighLoadContainerCount),
		ContainerStopTimeout:                 int32(data.ContainerStopTimeout),
		MaxLogSize:                           data.MaxLogSize,
		MaxLogFiles:                          data.MaxLogFiles,
		ContainerDiskQuota:                   data.ContainerDiskQuota,
		ReservedDomainPrefixes:               data.ReservedDomainPrefixes,
		MaxVolumesPerUser:                    int32(data.MaxVolumesPerUser),
		MaxContainersPerUser:                 int32(data.MaxContainersPerUser),
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
		MaxConcurrentBuilds:                  int32(data.MaxConcurrentBuilds),
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
		BuildOutboxBatchSize:                 int32(data.BuildOutboxBatchSize),
		ComposeUploadMaxBytes:                data.ComposeUploadMaxBytes,
		ComposePipelineTimeoutMinutes:        data.ComposePipelineTimeoutMinutes,
		ComposeDeployWorkerCount:             int32(data.ComposeDeployWorkerCount),
		ComposeOutboxIntervalSeconds:         data.ComposeOutboxIntervalSeconds,
		ComposeOutboxBatchSize:               int32(data.ComposeOutboxBatchSize),
		ComposeDeployMaxAttempts:             int32(data.ComposeDeployMaxAttempts),
		ComposeBuildPollIntervalSeconds:      data.ComposeBuildPollIntervalSeconds,
		ComposeDependencyWaitTimeoutMinutes:  data.ComposeDependencyWaitTimeoutMinutes,
		ComposeDependencyPollIntervalSeconds: data.ComposeDependencyPollIntervalSeconds,
		GitSourcesEnabled:                    data.GitSourcesEnabled,
		GitAllowedHosts:                      data.GitAllowedHosts,
		GitCloneTimeoutSeconds:               data.GitCloneTimeoutSeconds,
		GitMaxRepositoryBytes:                data.GitMaxRepositoryBytes,
		TelemetryMaxLogTailLines:             int32(data.TelemetryMaxLogTailLines),
		TelemetryMaxLogStreamsPerUser:        int32(data.TelemetryMaxLogStreamsPerUser),
		TelemetryMaxTerminalSessionsPerUser:  int32(data.TelemetryMaxTerminalSessionsPerUser),
		TelemetryTerminalIdleTimeoutSeconds:  data.TelemetryTerminalIdleTimeoutSeconds,
		TelemetryTerminalMaxDurationSeconds:  data.TelemetryTerminalMaxDurationSeconds,
		TelemetryAllowedExecCommands:         data.TelemetryAllowedExecCommands,
		TelemetryMaxCommandArgs:              int32(data.TelemetryMaxCommandArgs),
		TelemetryMaxCommandArgBytes:          int32(data.TelemetryMaxCommandArgBytes),
		TelemetryWsReadLimitBytes:            data.TelemetryWSReadLimitBytes,
	}
}
