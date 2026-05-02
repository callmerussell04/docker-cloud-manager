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
}

type StatsHandler struct {
	coreapi.UnimplementedStatsAPIServer
	logic StatsLogic
}

func RegisterStatsAPI(gRPCServer *grpc.Server, logic StatsLogic) {
	coreapi.RegisterStatsAPIServer(gRPCServer, &StatsHandler{logic: logic})
}

func (h *StatsHandler) GetUserStats(ctx context.Context, _ *coreapi.Empty) (*coreapi.UserStatsResponse, error) {
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
