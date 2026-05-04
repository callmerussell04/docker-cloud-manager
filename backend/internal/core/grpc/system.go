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
	cfg := h.logic.GetConfig(ctx)
	return &coreapi.SystemConfigData{
		BaseDomain:                           cfg.BaseDomain,
		DefaultMemoryReservationBytes:        cfg.DefaultMemoryReservation,
		ReservedSystemMemoryBytes:            cfg.ReservedSystemMemory,
		OvercommitFactor:                     cfg.OvercommitFactor,
		MaxBurstMultiplier:                   cfg.MaxBurstMultiplier,
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
		RegistryApiUrl:                       cfg.RegistryAPIURL,
		RegistryPublicUrl:                    cfg.RegistryPublicURL,
		ContainerTtlHours:                    cfg.ContainerTTLHours,
		ContainerPidsLimit:                   cfg.ContainerPidsLimit,
		ContainerMemorySwapMultiplier:        cfg.ContainerMemorySwapMultiplier,
		ProxyNetworkName:                     cfg.ProxyNetworkName,
		RegistryContainerName:                cfg.RegistryContainerName,
		ImageBuildsEnabled:                   cfg.ImageBuildsEnabled,
		BuildMemoryBytes:                     cfg.BuildMemoryBytes,
		BuildCpuQuota:                        cfg.BuildCPUQuota,
		BuildCpuPeriod:                       cfg.BuildCPUPeriod,
		BuildMemorySwapMultiplier:            cfg.BuildMemorySwapMultiplier,
		BuildPidsLimit:                       cfg.BuildPidsLimit,
		BuildNetworkName:                     cfg.BuildNetworkName,
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
		ComposeUploadMaxBytes:                cfg.ComposeUploadMaxBytes,
		ComposePipelineTimeoutMinutes:        cfg.ComposePipelineTimeoutMinutes,
		ComposeBuildPollIntervalSeconds:      cfg.ComposeBuildPollIntervalSeconds,
		ComposeDependencyWaitTimeoutMinutes:  cfg.ComposeDependencyWaitTimeoutMinutes,
		ComposeDependencyPollIntervalSeconds: cfg.ComposeDependencyPollIntervalSeconds,
		TelemetryMaxLogTailLines:             int32(cfg.TelemetryMaxLogTailLines),
		TelemetryMaxLogStreamsPerUser:        int32(cfg.TelemetryMaxLogStreamsPerUser),
		TelemetryMaxTerminalSessionsPerUser:  int32(cfg.TelemetryMaxTerminalSessionsPerUser),
		TelemetryTerminalIdleTimeoutSeconds:  cfg.TelemetryTerminalIdleTimeoutSeconds,
		TelemetryTerminalMaxDurationSeconds:  cfg.TelemetryTerminalMaxDurationSeconds,
		TelemetryAllowedExecCommands:         cfg.TelemetryAllowedExecCommands,
		TelemetryMaxCommandArgs:              int32(cfg.TelemetryMaxCommandArgs),
		TelemetryMaxCommandArgBytes:          int32(cfg.TelemetryMaxCommandArgBytes),
		TelemetryWsReadLimitBytes:            cfg.TelemetryWSReadLimitBytes,
	}, nil
}

func (h *SystemHandler) UpdateConfig(ctx context.Context, req *coreapi.SystemConfigData) (*coreapi.Empty, error) {
	newCfg := config.SystemConfig{
		BaseDomain:                           req.GetBaseDomain(),
		DefaultMemoryReservation:             req.GetDefaultMemoryReservationBytes(),
		ReservedSystemMemory:                 req.GetReservedSystemMemoryBytes(),
		OvercommitFactor:                     req.GetOvercommitFactor(),
		MaxBurstMultiplier:                   req.GetMaxBurstMultiplier(),
		DefaultCPUShares:                     req.GetDefaultCpuShares(),
		HighLoadCPUShares:                    req.GetHighLoadCpuShares(),
		HighLoadContainerCount:               int(req.GetHighLoadContainerCount()),
		ContainerStopTimeout:                 int(req.GetContainerStopTimeout()),
		MaxLogSize:                           req.GetMaxLogSize(),
		MaxLogFiles:                          req.GetMaxLogFiles(),
		ContainerDiskQuota:                   req.GetContainerDiskQuota(),
		ReservedDomainPrefixes:               req.GetReservedDomainPrefixes(),
		MaxVolumesPerUser:                    int(req.GetMaxVolumesPerUser()),
		MaxContainersPerUser:                 int(req.GetMaxContainersPerUser()),
		RegistryAPIURL:                       req.GetRegistryApiUrl(),
		RegistryPublicURL:                    req.GetRegistryPublicUrl(),
		ContainerTTLHours:                    req.GetContainerTtlHours(),
		ContainerPidsLimit:                   req.GetContainerPidsLimit(),
		ContainerMemorySwapMultiplier:        req.GetContainerMemorySwapMultiplier(),
		ProxyNetworkName:                     req.GetProxyNetworkName(),
		RegistryContainerName:                req.GetRegistryContainerName(),
		ImageBuildsEnabled:                   req.GetImageBuildsEnabled(),
		BuildMemoryBytes:                     req.GetBuildMemoryBytes(),
		BuildCPUQuota:                        req.GetBuildCpuQuota(),
		BuildCPUPeriod:                       req.GetBuildCpuPeriod(),
		BuildMemorySwapMultiplier:            req.GetBuildMemorySwapMultiplier(),
		BuildPidsLimit:                       req.GetBuildPidsLimit(),
		BuildNetworkName:                     req.GetBuildNetworkName(),
		KanikoImage:                          req.GetKanikoImage(),
		MaxBuildTimeMinutes:                  req.GetMaxBuildTimeMinutes(),
		MaxConcurrentBuilds:                  int(req.GetMaxConcurrentBuilds()),
		MaxUploadSizeBytes:                   req.GetMaxUploadSizeBytes(),
		MaxArchiveSizeBytes:                  req.GetMaxArchiveSizeBytes(),
		MaxUnpackedSizeBytes:                 req.GetMaxUnpackedSizeBytes(),
		MaxBuildLogSizeBytes:                 req.GetMaxBuildLogSizeBytes(),
		BuildCancelPollIntervalSeconds:       req.GetBuildCancelPollIntervalSeconds(),
		TTLWorkerIntervalSeconds:             req.GetTtlWorkerIntervalSeconds(),
		GCWorkerIntervalMinutes:              req.GetGcWorkerIntervalMinutes(),
		StaleBuildTimeoutMinutes:             req.GetStaleBuildTimeoutMinutes(),
		EventSyncIntervalSeconds:             req.GetEventSyncIntervalSeconds(),
		EventReconnectDelaySeconds:           req.GetEventReconnectDelaySeconds(),
		BuildOutboxIntervalSeconds:           req.GetBuildOutboxIntervalSeconds(),
		BuildOutboxBatchSize:                 int(req.GetBuildOutboxBatchSize()),
		ComposeUploadMaxBytes:                req.GetComposeUploadMaxBytes(),
		ComposePipelineTimeoutMinutes:        req.GetComposePipelineTimeoutMinutes(),
		ComposeBuildPollIntervalSeconds:      req.GetComposeBuildPollIntervalSeconds(),
		ComposeDependencyWaitTimeoutMinutes:  req.GetComposeDependencyWaitTimeoutMinutes(),
		ComposeDependencyPollIntervalSeconds: req.GetComposeDependencyPollIntervalSeconds(),
		TelemetryMaxLogTailLines:             int(req.GetTelemetryMaxLogTailLines()),
		TelemetryMaxLogStreamsPerUser:        int(req.GetTelemetryMaxLogStreamsPerUser()),
		TelemetryMaxTerminalSessionsPerUser:  int(req.GetTelemetryMaxTerminalSessionsPerUser()),
		TelemetryTerminalIdleTimeoutSeconds:  req.GetTelemetryTerminalIdleTimeoutSeconds(),
		TelemetryTerminalMaxDurationSeconds:  req.GetTelemetryTerminalMaxDurationSeconds(),
		TelemetryAllowedExecCommands:         req.GetTelemetryAllowedExecCommands(),
		TelemetryMaxCommandArgs:              int(req.GetTelemetryMaxCommandArgs()),
		TelemetryMaxCommandArgBytes:          int(req.GetTelemetryMaxCommandArgBytes()),
		TelemetryWSReadLimitBytes:            req.GetTelemetryWsReadLimitBytes(),
	}

	if err := h.logic.UpdateConfig(ctx, newCfg); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.Empty{}, nil
}
