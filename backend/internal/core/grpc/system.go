package grpc

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
	"google.golang.org/grpc"
)

type SystemLogic interface {
	GetConfig(ctx context.Context) config.SystemConfig
	UpdateConfig(ctx context.Context, newConfig config.SystemConfig) error
}

type SystemHandler struct {
	coreapi.UnimplementedSystemAPIServer
	logic SystemLogic
}

func RegisterSystemAPI(gRPCServer *grpc.Server, logic SystemLogic) {
	coreapi.RegisterSystemAPIServer(gRPCServer, &SystemHandler{logic: logic})
}

func (h *SystemHandler) GetConfig(ctx context.Context, _ *coreapi.Empty) (*coreapi.SystemConfigData, error) {
	if err := requireAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	cfg := h.logic.GetConfig(ctx)
	return &coreapi.SystemConfigData{
		BaseDomain:                           cfg.BaseDomain,
		DefaultMemoryReservationBytes:        cfg.DefaultMemoryReservation,
		ReservedSystemMemoryBytes:            cfg.ReservedSystemMemory,
		OvercommitFactor:                     cfg.OvercommitFactor,
		MaxBurstMultiplier:                   cfg.MaxBurstMultiplier,
		DefaultCpuReservationMillicores:      cfg.DefaultCPUReservation,
		ReservedSystemCpuMillicores:          cfg.ReservedSystemCPU,
		CpuOvercommitFactor:                  cfg.CPUOvercommitFactor,
		MaxCpuBurstMultiplier:                cfg.MaxCPUBurstMultiplier,
		ContainerCpuPeriod:                   cfg.ContainerCPUPeriod,
		DefaultCpuShares:                     cfg.DefaultCPUShares,
		HighLoadCpuShares:                    cfg.HighLoadCPUShares,
		HighLoadContainerCount:               int32(cfg.HighLoadContainerCount),
		ContainerStopTimeout:                 int32(cfg.ContainerStopTimeout),
		MaxLogSize:                           cfg.MaxLogSize,
		MaxLogFiles:                          cfg.MaxLogFiles,
		ContainerDiskQuota:                   cfg.ContainerDiskQuota,
		ReservedDomainPrefixes:               cfg.ReservedDomainPrefixes,
		MaxVolumesPerUser:                    int32(cfg.MaxVolumesPerUser),
		MaxContainersPerUser:                 int32(cfg.MaxContainersPerUser),
		ContainerTtlHours:                    cfg.ContainerTTLHours,
		ContainerPidsLimit:                   cfg.ContainerPidsLimit,
		ContainerMemorySwapMultiplier:        cfg.ContainerMemorySwapMultiplier,
		ImageBuildsEnabled:                   cfg.ImageBuildsEnabled,
		BuildMemoryBytes:                     cfg.BuildMemoryBytes,
		BuildCpuQuota:                        cfg.BuildCPUQuota,
		BuildCpuPeriod:                       cfg.BuildCPUPeriod,
		BuildMemorySwapMultiplier:            cfg.BuildMemorySwapMultiplier,
		BuildPidsLimit:                       cfg.BuildPidsLimit,
		KanikoImage:                          cfg.KanikoImage,
		MaxBuildTimeMinutes:                  cfg.MaxBuildTimeMinutes,
		MaxConcurrentBuilds:                  int32(cfg.MaxConcurrentBuilds),
		MaxUploadSizeBytes:                   cfg.MaxUploadSizeBytes,
		MaxArchiveSizeBytes:                  cfg.MaxArchiveSizeBytes,
		MaxUnpackedSizeBytes:                 cfg.MaxUnpackedSizeBytes,
		MaxBuildLogSizeBytes:                 cfg.MaxBuildLogSizeBytes,
		BuildCancelPollIntervalSeconds:       cfg.BuildCancelPollIntervalSeconds,
		TtlWorkerIntervalSeconds:             cfg.TTLWorkerIntervalSeconds,
		GcWorkerIntervalMinutes:              cfg.GCWorkerIntervalMinutes,
		StaleBuildTimeoutMinutes:             cfg.StaleBuildTimeoutMinutes,
		EventSyncIntervalSeconds:             cfg.EventSyncIntervalSeconds,
		EventReconnectDelaySeconds:           cfg.EventReconnectDelaySeconds,
		BuildOutboxIntervalSeconds:           cfg.BuildOutboxIntervalSeconds,
		BuildOutboxBatchSize:                 int32(cfg.BuildOutboxBatchSize),
		ContainerCreateWorkerCount:           int32(cfg.ContainerCreateWorkerCount),
		ContainerCreateMaxAttempts:           int32(cfg.ContainerCreateMaxAttempts),
		ContainerCreateTimeoutMinutes:        cfg.ContainerCreateTimeoutMinutes,
		MaxQueuedContainerCreatesPerUser:     int32(cfg.MaxQueuedContainerCreatesPerUser),
		ContainerCreateOutboxIntervalSeconds: cfg.ContainerCreateOutboxIntervalSeconds,
		ContainerCreateOutboxBatchSize:       int32(cfg.ContainerCreateOutboxBatchSize),
		ReportsUsageSnapshotIntervalSeconds:  cfg.ReportsUsageSnapshotIntervalSeconds,
		MaxStagedSourceBytesPerUser:          cfg.MaxStagedSourceBytesPerUser,
		MaxQueuedBuildsPerUser:               int32(cfg.MaxQueuedBuildsPerUser),
		ComposeUploadMaxBytes:                cfg.ComposeUploadMaxBytes,
		ComposePipelineTimeoutMinutes:        cfg.ComposePipelineTimeoutMinutes,
		ComposeDeployWorkerCount:             int32(cfg.ComposeDeployWorkerCount),
		ComposeOutboxIntervalSeconds:         cfg.ComposeOutboxIntervalSeconds,
		ComposeOutboxBatchSize:               int32(cfg.ComposeOutboxBatchSize),
		ComposeDeployMaxAttempts:             int32(cfg.ComposeDeployMaxAttempts),
		MaxQueuedComposeDeploysPerUser:       int32(cfg.MaxQueuedComposeDeploysPerUser),
		ComposeBuildPollIntervalSeconds:      cfg.ComposeBuildPollIntervalSeconds,
		ComposeDependencyWaitTimeoutMinutes:  cfg.ComposeDependencyWaitTimeoutMinutes,
		ComposeDependencyPollIntervalSeconds: cfg.ComposeDependencyPollIntervalSeconds,
		ComposeCoordinatorIntervalSeconds:    cfg.ComposeCoordinatorIntervalSeconds,
		HostMinFreeDiskBytes:                 cfg.HostMinFreeDiskBytes,
		TelemetryMaxLogTailLines:             int32(cfg.TelemetryMaxLogTailLines),
		TelemetryMaxLogStreamsPerUser:        int32(cfg.TelemetryMaxLogStreamsPerUser),
		TelemetryMaxTerminalSessionsPerUser:  int32(cfg.TelemetryMaxTerminalSessionsPerUser),
		TelemetryTerminalIdleTimeoutSeconds:  cfg.TelemetryTerminalIdleTimeoutSeconds,
		TelemetryTerminalMaxDurationSeconds:  cfg.TelemetryTerminalMaxDurationSeconds,
		TelemetryAllowedExecCommands:         cfg.TelemetryAllowedExecCommands,
		TelemetryMaxCommandArgs:              int32(cfg.TelemetryMaxCommandArgs),
		TelemetryMaxCommandArgBytes:          int32(cfg.TelemetryMaxCommandArgBytes),
		TelemetryWsReadLimitBytes:            cfg.TelemetryWSReadLimitBytes,
		GitSourcesEnabled:                    cfg.GitSourcesEnabled,
		GitAllowedHosts:                      cfg.GitAllowedHosts,
		GitCloneTimeoutSeconds:               cfg.GitCloneTimeoutSeconds,
		GitMaxRepositoryBytes:                cfg.GitMaxRepositoryBytes,
	}, nil
}

func (h *SystemHandler) UpdateConfig(ctx context.Context, req *coreapi.SystemConfigData) (*coreapi.Empty, error) {
	if err := requireAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	newCfg := h.logic.GetConfig(ctx)
	newCfg.BaseDomain = req.GetBaseDomain()
	newCfg.DefaultMemoryReservation = req.GetDefaultMemoryReservationBytes()
	newCfg.ReservedSystemMemory = req.GetReservedSystemMemoryBytes()
	newCfg.OvercommitFactor = req.GetOvercommitFactor()
	newCfg.MaxBurstMultiplier = req.GetMaxBurstMultiplier()
	newCfg.DefaultCPUReservation = req.GetDefaultCpuReservationMillicores()
	newCfg.ReservedSystemCPU = req.GetReservedSystemCpuMillicores()
	newCfg.CPUOvercommitFactor = req.GetCpuOvercommitFactor()
	newCfg.MaxCPUBurstMultiplier = req.GetMaxCpuBurstMultiplier()
	newCfg.ContainerCPUPeriod = req.GetContainerCpuPeriod()
	newCfg.DefaultCPUShares = req.GetDefaultCpuShares()
	newCfg.HighLoadCPUShares = req.GetHighLoadCpuShares()
	newCfg.HighLoadContainerCount = int(req.GetHighLoadContainerCount())
	newCfg.ContainerStopTimeout = int(req.GetContainerStopTimeout())
	newCfg.MaxLogSize = req.GetMaxLogSize()
	newCfg.MaxLogFiles = req.GetMaxLogFiles()
	newCfg.ContainerDiskQuota = req.GetContainerDiskQuota()
	newCfg.ReservedDomainPrefixes = req.GetReservedDomainPrefixes()
	newCfg.MaxVolumesPerUser = int(req.GetMaxVolumesPerUser())
	newCfg.MaxContainersPerUser = int(req.GetMaxContainersPerUser())
	newCfg.ContainerTTLHours = req.GetContainerTtlHours()
	newCfg.ContainerPidsLimit = req.GetContainerPidsLimit()
	newCfg.ContainerMemorySwapMultiplier = req.GetContainerMemorySwapMultiplier()
	newCfg.ImageBuildsEnabled = req.GetImageBuildsEnabled()
	newCfg.BuildMemoryBytes = req.GetBuildMemoryBytes()
	newCfg.BuildCPUQuota = req.GetBuildCpuQuota()
	newCfg.BuildCPUPeriod = req.GetBuildCpuPeriod()
	newCfg.BuildMemorySwapMultiplier = req.GetBuildMemorySwapMultiplier()
	newCfg.BuildPidsLimit = req.GetBuildPidsLimit()
	newCfg.KanikoImage = req.GetKanikoImage()
	newCfg.MaxBuildTimeMinutes = req.GetMaxBuildTimeMinutes()
	newCfg.MaxConcurrentBuilds = int(req.GetMaxConcurrentBuilds())
	newCfg.MaxUploadSizeBytes = req.GetMaxUploadSizeBytes()
	newCfg.MaxArchiveSizeBytes = req.GetMaxArchiveSizeBytes()
	newCfg.MaxUnpackedSizeBytes = req.GetMaxUnpackedSizeBytes()
	newCfg.MaxBuildLogSizeBytes = req.GetMaxBuildLogSizeBytes()
	newCfg.BuildCancelPollIntervalSeconds = req.GetBuildCancelPollIntervalSeconds()
	newCfg.TTLWorkerIntervalSeconds = req.GetTtlWorkerIntervalSeconds()
	newCfg.GCWorkerIntervalMinutes = req.GetGcWorkerIntervalMinutes()
	newCfg.StaleBuildTimeoutMinutes = req.GetStaleBuildTimeoutMinutes()
	newCfg.EventSyncIntervalSeconds = req.GetEventSyncIntervalSeconds()
	newCfg.EventReconnectDelaySeconds = req.GetEventReconnectDelaySeconds()
	newCfg.BuildOutboxIntervalSeconds = req.GetBuildOutboxIntervalSeconds()
	newCfg.BuildOutboxBatchSize = int(req.GetBuildOutboxBatchSize())
	newCfg.ContainerCreateWorkerCount = int(req.GetContainerCreateWorkerCount())
	newCfg.ContainerCreateMaxAttempts = int(req.GetContainerCreateMaxAttempts())
	newCfg.ContainerCreateTimeoutMinutes = req.GetContainerCreateTimeoutMinutes()
	newCfg.MaxQueuedContainerCreatesPerUser = int(req.GetMaxQueuedContainerCreatesPerUser())
	newCfg.ContainerCreateOutboxIntervalSeconds = req.GetContainerCreateOutboxIntervalSeconds()
	newCfg.ContainerCreateOutboxBatchSize = int(req.GetContainerCreateOutboxBatchSize())
	newCfg.ReportsUsageSnapshotIntervalSeconds = req.GetReportsUsageSnapshotIntervalSeconds()
	newCfg.MaxStagedSourceBytesPerUser = req.GetMaxStagedSourceBytesPerUser()
	newCfg.MaxQueuedBuildsPerUser = int(req.GetMaxQueuedBuildsPerUser())
	newCfg.ComposeUploadMaxBytes = req.GetComposeUploadMaxBytes()
	newCfg.ComposePipelineTimeoutMinutes = req.GetComposePipelineTimeoutMinutes()
	newCfg.ComposeDeployWorkerCount = int(req.GetComposeDeployWorkerCount())
	newCfg.ComposeOutboxIntervalSeconds = req.GetComposeOutboxIntervalSeconds()
	newCfg.ComposeOutboxBatchSize = int(req.GetComposeOutboxBatchSize())
	newCfg.ComposeDeployMaxAttempts = int(req.GetComposeDeployMaxAttempts())
	newCfg.MaxQueuedComposeDeploysPerUser = int(req.GetMaxQueuedComposeDeploysPerUser())
	newCfg.ComposeBuildPollIntervalSeconds = req.GetComposeBuildPollIntervalSeconds()
	newCfg.ComposeDependencyWaitTimeoutMinutes = req.GetComposeDependencyWaitTimeoutMinutes()
	newCfg.ComposeDependencyPollIntervalSeconds = req.GetComposeDependencyPollIntervalSeconds()
	newCfg.ComposeCoordinatorIntervalSeconds = req.GetComposeCoordinatorIntervalSeconds()
	newCfg.HostMinFreeDiskBytes = req.GetHostMinFreeDiskBytes()
	newCfg.TelemetryMaxLogTailLines = int(req.GetTelemetryMaxLogTailLines())
	newCfg.TelemetryMaxLogStreamsPerUser = int(req.GetTelemetryMaxLogStreamsPerUser())
	newCfg.TelemetryMaxTerminalSessionsPerUser = int(req.GetTelemetryMaxTerminalSessionsPerUser())
	newCfg.TelemetryTerminalIdleTimeoutSeconds = req.GetTelemetryTerminalIdleTimeoutSeconds()
	newCfg.TelemetryTerminalMaxDurationSeconds = req.GetTelemetryTerminalMaxDurationSeconds()
	newCfg.TelemetryAllowedExecCommands = req.GetTelemetryAllowedExecCommands()
	newCfg.TelemetryMaxCommandArgs = int(req.GetTelemetryMaxCommandArgs())
	newCfg.TelemetryMaxCommandArgBytes = int(req.GetTelemetryMaxCommandArgBytes())
	newCfg.TelemetryWSReadLimitBytes = req.GetTelemetryWsReadLimitBytes()
	newCfg.GitSourcesEnabled = req.GetGitSourcesEnabled()
	newCfg.GitAllowedHosts = req.GetGitAllowedHosts()
	newCfg.GitCloneTimeoutSeconds = req.GetGitCloneTimeoutSeconds()
	newCfg.GitMaxRepositoryBytes = req.GetGitMaxRepositoryBytes()
	if err := h.logic.UpdateConfig(ctx, newCfg); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.Empty{}, nil
}

func (h *SystemHandler) GetBuilderRuntimeConfig(ctx context.Context, _ *coreapi.Empty) (*coreapi.BuilderRuntimeConfigData, error) {
	if err := requireSystemScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	cfg := h.logic.GetConfig(ctx)
	return &coreapi.BuilderRuntimeConfigData{
		BuildMemoryBytes:               cfg.BuildMemoryBytes,
		BuildCpuQuota:                  cfg.BuildCPUQuota,
		BuildCpuPeriod:                 cfg.BuildCPUPeriod,
		BuildMemorySwapMultiplier:      cfg.BuildMemorySwapMultiplier,
		BuildPidsLimit:                 cfg.BuildPidsLimit,
		BuildNetworkName:               cfg.BuildNetworkName,
		KanikoImage:                    cfg.KanikoImage,
		MaxBuildTimeMinutes:            cfg.MaxBuildTimeMinutes,
		MaxConcurrentBuilds:            int32(cfg.MaxConcurrentBuilds),
		MaxUnpackedSizeBytes:           cfg.MaxUnpackedSizeBytes,
		MaxBuildLogSizeBytes:           cfg.MaxBuildLogSizeBytes,
		BuildCancelPollIntervalSeconds: cfg.BuildCancelPollIntervalSeconds,
	}, nil
}

func (h *SystemHandler) GetTelemetryRuntimeConfig(ctx context.Context, _ *coreapi.Empty) (*coreapi.TelemetryRuntimeConfigData, error) {
	if err := requireSystemScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	cfg := h.logic.GetConfig(ctx)
	return &coreapi.TelemetryRuntimeConfigData{
		TelemetryMaxLogTailLines:            int32(cfg.TelemetryMaxLogTailLines),
		TelemetryMaxLogStreamsPerUser:       int32(cfg.TelemetryMaxLogStreamsPerUser),
		TelemetryMaxTerminalSessionsPerUser: int32(cfg.TelemetryMaxTerminalSessionsPerUser),
		TelemetryTerminalIdleTimeoutSeconds: cfg.TelemetryTerminalIdleTimeoutSeconds,
		TelemetryTerminalMaxDurationSeconds: cfg.TelemetryTerminalMaxDurationSeconds,
		TelemetryAllowedExecCommands:        cfg.TelemetryAllowedExecCommands,
		TelemetryMaxCommandArgs:             int32(cfg.TelemetryMaxCommandArgs),
		TelemetryMaxCommandArgBytes:         int32(cfg.TelemetryMaxCommandArgBytes),
		TelemetryWsReadLimitBytes:           cfg.TelemetryWSReadLimitBytes,
	}, nil
}
