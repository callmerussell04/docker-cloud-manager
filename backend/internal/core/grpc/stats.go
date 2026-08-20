package grpc

import (
	"context"

	"google.golang.org/grpc"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
)

type StatsLogic interface {
	GetUserStats(ctx context.Context) (model.UserStats, error)
	GetSystemMonitoring(ctx context.Context) (model.SystemMonitoring, error)
}

type StatsHandler struct {
	coreapi.UnimplementedStatsAPIServer
	logic StatsLogic
}

func RegisterStatsAPI(gRPCServer *grpc.Server, logic StatsLogic) {
	coreapi.RegisterStatsAPIServer(gRPCServer, &StatsHandler{logic: logic})
}

func (h *StatsHandler) GetUserStats(ctx context.Context, _ *coreapi.Empty) (*coreapi.UserStatsResponse, error) {
	if err := requireUserScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	stats, err := h.logic.GetUserStats(ctx)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.UserStatsResponse{
		ContainersTotal:   int32(stats.ContainersTotal),
		ContainersRunning: int32(stats.ContainersRunning),
		ContainersQuota:   int32(stats.ContainersQuota),
		RamUsedBytes:      stats.RamUsedBytes,
		RamQuotaBytes:     stats.RamQuotaBytes,
		DiskUsedMb:        int32(stats.DiskUsedMB),
		DiskQuotaMb:       int32(stats.DiskQuotaMB),
		VolumesTotal:      int32(stats.VolumesTotal),
		VolumesQuota:      int32(stats.VolumesQuota),
		ImagesTotal:       int32(stats.ImagesTotal),
		ProjectsTotal:     int32(stats.ProjectsTotal),
	}, nil
}

func (h *StatsHandler) GetSystemMonitoring(ctx context.Context, _ *coreapi.Empty) (*coreapi.SystemMonitoringResponse, error) {
	if err := requireAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	stats, err := h.logic.GetSystemMonitoring(ctx)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.SystemMonitoringResponse{
		CpuPercent:                  stats.CPUPercent,
		MemoryTotalBytes:            stats.MemoryTotalBytes,
		MemoryUsedBytes:             stats.MemoryUsedBytes,
		MemoryAvailableBytes:        stats.MemoryAvailableBytes,
		DiskTotalBytes:              stats.DiskTotalBytes,
		DiskUsedBytes:               stats.DiskUsedBytes,
		DiskFreeBytes:               stats.DiskFreeBytes,
		DcmReservedMemoryBytes:      stats.DCMReservedMemoryBytes,
		DcmReservedBuildMemoryBytes: stats.DCMReservedBuildMemoryBytes,
		DcmDiskUsedBytes:            stats.DCMDiskUsedBytes,
		HostMinFreeDiskBytes:        stats.HostMinFreeDiskBytes,
		AdmissionStatus:             stats.AdmissionStatus,
		AdmissionReasons:            stats.AdmissionReasons,
		ContainersTotal:             int32(stats.ContainersTotal),
		ContainersRunning:           int32(stats.ContainersRunning),
		ContainersStopped:           int32(stats.ContainersStopped),
		ContainersError:             int32(stats.ContainersError),
		ContainersMissing:           int32(stats.ContainersMissing),
		VolumesTotal:                int32(stats.VolumesTotal),
		ImagesTotal:                 int32(stats.ImagesTotal),
		BuildsTotal:                 int32(stats.BuildsTotal),
		ProjectsTotal:               int32(stats.ProjectsTotal),
		ObservedAt:                  stats.ObservedAt.Unix(),
	}, nil
}
