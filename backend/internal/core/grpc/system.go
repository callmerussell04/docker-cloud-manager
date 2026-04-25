package grpc

import (
	"context"
	"time"

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
		BaseDomain:                    cfg.BaseDomain,
		DefaultMemoryReservationBytes: cfg.DefaultMemoryReservation,
		ReservedSystemMemoryBytes:     cfg.ReservedSystemMemory,
		OvercommitFactor:              cfg.OvercommitFactor,
		MaxBurstMultiplier:            cfg.MaxBurstMultiplier,
		DefaultCpuShares:              cfg.DefaultCPUShares,
		HighLoadCpuShares:             cfg.HighLoadCPUShares,
		HighLoadContainerCount:        int32(cfg.HighLoadContainerCount),
		ContainerStopTimeout:          int32(cfg.ContainerStopTimeout),
		MaxLogSize:                    cfg.MaxLogSize,
		MaxLogFiles:                   cfg.MaxLogFiles,
		ContainerDiskQuota:            cfg.ContainerDiskQuota,
		MaxVolumesPerUser:             int32(cfg.MaxVolumesPerUser),
		MaxContainersPerUser:          int32(cfg.MaxContainersPerUser),
		RegistryApiUrl:                cfg.RegistryAPIURL,
		RegistryPublicUrl:             cfg.RegistryPublicURL,
		ContainerTtlHours:             int64(cfg.ContainerTTL.Hours()),
	}, nil
}

func (h *SystemHandler) UpdateConfig(ctx context.Context, req *coreapi.SystemConfigData) (*coreapi.Empty, error) {
	newCfg := config.SystemConfig{
		BaseDomain:               req.GetBaseDomain(),
		DefaultMemoryReservation: req.GetDefaultMemoryReservationBytes(),
		ReservedSystemMemory:     req.GetReservedSystemMemoryBytes(),
		OvercommitFactor:         req.GetOvercommitFactor(),
		MaxBurstMultiplier:       req.GetMaxBurstMultiplier(),
		DefaultCPUShares:         req.GetDefaultCpuShares(),
		HighLoadCPUShares:        req.GetHighLoadCpuShares(),
		HighLoadContainerCount:   int(req.GetHighLoadContainerCount()),
		ContainerStopTimeout:     int(req.GetContainerStopTimeout()),
		MaxLogSize:               req.GetMaxLogSize(),
		MaxLogFiles:              req.GetMaxLogFiles(),
		ContainerDiskQuota:       req.GetContainerDiskQuota(),
		MaxVolumesPerUser:        int(req.GetMaxVolumesPerUser()),
		MaxContainersPerUser:     int(req.GetMaxContainersPerUser()),
		RegistryAPIURL:           req.GetRegistryApiUrl(),
		RegistryPublicURL:        req.GetRegistryPublicUrl(),
		ContainerTTL:             time.Duration(req.GetContainerTtlHours()) * time.Hour,
	}

	if err := h.logic.UpdateConfig(ctx, newCfg); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.Empty{}, nil
}
