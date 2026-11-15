package grpc

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/backend/api/core"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/grpcerrors"
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
	return systemConfigToProto(h.logic.GetConfig(ctx)), nil
}

func (h *SystemHandler) UpdateConfig(ctx context.Context, req *coreapi.SystemConfigData) (*coreapi.Empty, error) {
	if err := requireAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	// The update starts from the current snapshot, so the fixed part an
	// administrator cannot change is carried over as it is.
	newCfg := h.logic.GetConfig(ctx)
	applyRuntimeSettings(&newCfg, req)
	if err := h.logic.UpdateConfig(ctx, newCfg); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.Empty{}, nil
}

// systemConfigToProto and applyRuntimeSettings carry every runtime setting,
// and nothing of the fixed part, between the snapshot and the message.
func systemConfigToProto(cfg config.SystemConfig) *coreapi.SystemConfigData {
	return &coreapi.SystemConfigData{
		ReservedSystemMemoryBytes:           cfg.ReservedSystemMemory,
		OvercommitFactor:                    cfg.OvercommitFactor,
		ReservedSystemCpuMillicores:         cfg.ReservedSystemCPU,
		CpuOvercommitFactor:                 cfg.CPUOvercommitFactor,
		HostMinFreeDiskBytes:                cfg.HostMinFreeDiskBytes,
		DefaultMemoryReservationBytes:       cfg.DefaultMemoryReservation,
		MaxBurstMultiplier:                  cfg.MaxBurstMultiplier,
		DefaultCpuReservationMillicores:     cfg.DefaultCPUReservation,
		MaxCpuBurstMultiplier:               cfg.MaxCPUBurstMultiplier,
		MemoryPressureMinFreeBytes:          cfg.MemoryPressureMinFreeBytes,
		MemoryBoostMultiplier:               cfg.MemoryBoostMultiplier,
		CpuBoostMultiplier:                  cfg.CPUBoostMultiplier,
		BoostMinFreeMemoryBytes:             cfg.BoostMinFreeMemoryBytes,
		ContainerTtlHours:                   cfg.ContainerTTLHours,
		ContainerStopTimeout:                int32(cfg.ContainerStopTimeout),
		ContainerPidsLimit:                  cfg.ContainerPidsLimit,
		ContainerDiskQuota:                  cfg.ContainerDiskQuota,
		MaxLogSize:                          cfg.MaxLogSize,
		DefaultVolumeSizeBytes:              cfg.DefaultVolumeSizeBytes,
		VolumeSizeEnforcement:               cfg.VolumeSizeEnforcement,
		DefaultUserQuotaCpu:                 cfg.DefaultUserQuotaCPU,
		DefaultUserQuotaRamMb:               cfg.DefaultUserQuotaRAMMB,
		DefaultUserQuotaDiskMb:              cfg.DefaultUserQuotaDiskMB,
		MaxContainersPerUser:                int32(cfg.MaxContainersPerUser),
		MaxVolumesPerUser:                   int32(cfg.MaxVolumesPerUser),
		MaxQueuedBuildsPerUser:              int32(cfg.MaxQueuedBuildsPerUser),
		MaxQueuedComposeDeploysPerUser:      int32(cfg.MaxQueuedComposeDeploysPerUser),
		MaxStagedSourceBytesPerUser:         cfg.MaxStagedSourceBytesPerUser,
		ImageBuildsEnabled:                  cfg.ImageBuildsEnabled,
		MaxConcurrentBuilds:                 int32(cfg.MaxConcurrentBuilds),
		BuildMemoryBytes:                    cfg.BuildMemoryBytes,
		BuildCpuMillicores:                  cfg.BuildCPUMillicores,
		BuildMaxDiskBytes:                   cfg.BuildMaxDiskBytes,
		MaxBuildTimeMinutes:                 cfg.MaxBuildTimeMinutes,
		MaxBuildLogSizeBytes:                cfg.MaxBuildLogSizeBytes,
		MaxArchiveSizeBytes:                 cfg.MaxArchiveSizeBytes,
		MaxUnpackedSizeBytes:                cfg.MaxUnpackedSizeBytes,
		GitSourcesEnabled:                   cfg.GitSourcesEnabled,
		GitAllowedHosts:                     cfg.GitAllowedHosts,
		GitCloneTimeoutSeconds:              cfg.GitCloneTimeoutSeconds,
		GitMaxRepositoryBytes:               cfg.GitMaxRepositoryBytes,
		ComposeUploadMaxBytes:               cfg.ComposeUploadMaxBytes,
		ComposePipelineTimeoutMinutes:       cfg.ComposePipelineTimeoutMinutes,
		ComposeDependencyWaitTimeoutMinutes: cfg.ComposeDependencyWaitTimeoutMinutes,
		ReservedDomainPrefixes:              cfg.ReservedDomainPrefixes,
		BlockedDomainPrefixPatterns:         cfg.BlockedDomainPrefixPatterns,
		TelemetryAllowedExecCommands:        cfg.TelemetryAllowedExecCommands,
		TelemetryTerminalIdleTimeoutSeconds: cfg.TelemetryTerminalIdleTimeoutSeconds,
		TelemetryTerminalMaxDurationSeconds: cfg.TelemetryTerminalMaxDurationSeconds,
	}
}

func applyRuntimeSettings(cfg *config.SystemConfig, req *coreapi.SystemConfigData) {
	cfg.ReservedSystemMemory = req.GetReservedSystemMemoryBytes()
	cfg.OvercommitFactor = req.GetOvercommitFactor()
	cfg.ReservedSystemCPU = req.GetReservedSystemCpuMillicores()
	cfg.CPUOvercommitFactor = req.GetCpuOvercommitFactor()
	cfg.HostMinFreeDiskBytes = req.GetHostMinFreeDiskBytes()
	cfg.DefaultMemoryReservation = req.GetDefaultMemoryReservationBytes()
	cfg.MaxBurstMultiplier = req.GetMaxBurstMultiplier()
	cfg.DefaultCPUReservation = req.GetDefaultCpuReservationMillicores()
	cfg.MaxCPUBurstMultiplier = req.GetMaxCpuBurstMultiplier()
	cfg.MemoryPressureMinFreeBytes = req.GetMemoryPressureMinFreeBytes()
	cfg.MemoryBoostMultiplier = req.GetMemoryBoostMultiplier()
	cfg.CPUBoostMultiplier = req.GetCpuBoostMultiplier()
	cfg.BoostMinFreeMemoryBytes = req.GetBoostMinFreeMemoryBytes()
	cfg.ContainerTTLHours = req.GetContainerTtlHours()
	cfg.ContainerStopTimeout = int(req.GetContainerStopTimeout())
	cfg.ContainerPidsLimit = req.GetContainerPidsLimit()
	cfg.ContainerDiskQuota = req.GetContainerDiskQuota()
	cfg.MaxLogSize = req.GetMaxLogSize()
	cfg.DefaultVolumeSizeBytes = req.GetDefaultVolumeSizeBytes()
	cfg.VolumeSizeEnforcement = req.GetVolumeSizeEnforcement()
	cfg.DefaultUserQuotaCPU = req.GetDefaultUserQuotaCpu()
	cfg.DefaultUserQuotaRAMMB = req.GetDefaultUserQuotaRamMb()
	cfg.DefaultUserQuotaDiskMB = req.GetDefaultUserQuotaDiskMb()
	cfg.MaxContainersPerUser = int(req.GetMaxContainersPerUser())
	cfg.MaxVolumesPerUser = int(req.GetMaxVolumesPerUser())
	cfg.MaxQueuedBuildsPerUser = int(req.GetMaxQueuedBuildsPerUser())
	cfg.MaxQueuedComposeDeploysPerUser = int(req.GetMaxQueuedComposeDeploysPerUser())
	cfg.MaxStagedSourceBytesPerUser = req.GetMaxStagedSourceBytesPerUser()
	cfg.ImageBuildsEnabled = req.GetImageBuildsEnabled()
	cfg.MaxConcurrentBuilds = int(req.GetMaxConcurrentBuilds())
	cfg.BuildMemoryBytes = req.GetBuildMemoryBytes()
	cfg.BuildCPUMillicores = req.GetBuildCpuMillicores()
	cfg.BuildMaxDiskBytes = req.GetBuildMaxDiskBytes()
	cfg.MaxBuildTimeMinutes = req.GetMaxBuildTimeMinutes()
	cfg.MaxBuildLogSizeBytes = req.GetMaxBuildLogSizeBytes()
	cfg.MaxArchiveSizeBytes = req.GetMaxArchiveSizeBytes()
	cfg.MaxUnpackedSizeBytes = req.GetMaxUnpackedSizeBytes()
	cfg.GitSourcesEnabled = req.GetGitSourcesEnabled()
	cfg.GitAllowedHosts = req.GetGitAllowedHosts()
	cfg.GitCloneTimeoutSeconds = req.GetGitCloneTimeoutSeconds()
	cfg.GitMaxRepositoryBytes = req.GetGitMaxRepositoryBytes()
	cfg.ComposeUploadMaxBytes = req.GetComposeUploadMaxBytes()
	cfg.ComposePipelineTimeoutMinutes = req.GetComposePipelineTimeoutMinutes()
	cfg.ComposeDependencyWaitTimeoutMinutes = req.GetComposeDependencyWaitTimeoutMinutes()
	cfg.ReservedDomainPrefixes = req.GetReservedDomainPrefixes()
	cfg.BlockedDomainPrefixPatterns = req.GetBlockedDomainPrefixPatterns()
	cfg.TelemetryAllowedExecCommands = req.GetTelemetryAllowedExecCommands()
	cfg.TelemetryTerminalIdleTimeoutSeconds = req.GetTelemetryTerminalIdleTimeoutSeconds()
	cfg.TelemetryTerminalMaxDurationSeconds = req.GetTelemetryTerminalMaxDurationSeconds()
}

func (h *SystemHandler) GetBuilderRuntimeConfig(ctx context.Context, _ *coreapi.Empty) (*coreapi.BuilderRuntimeConfigData, error) {
	if err := requireSystemScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	cfg := h.logic.GetConfig(ctx)
	return &coreapi.BuilderRuntimeConfigData{
		BuildMemoryBytes:               cfg.BuildMemoryBytes,
		BuildCpuQuota:                  cfg.BuildCPUQuota(),
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
		BuildMaxDiskBytes:              cfg.BuildMaxDiskBytes,
	}, nil
}
