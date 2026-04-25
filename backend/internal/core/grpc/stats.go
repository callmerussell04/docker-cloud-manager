package grpc

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type StatsLogic interface {
	GetUserStats(ctx context.Context, ownerID uuid.UUID) (model.UserStats, error)
}

type StatsHandler struct {
	coreapi.UnimplementedStatsAPIServer
	logic StatsLogic
}

func RegisterStatsAPI(gRPCServer *grpc.Server, logic StatsLogic) {
	coreapi.RegisterStatsAPIServer(gRPCServer, &StatsHandler{logic: logic})
}

func (h *StatsHandler) GetUserStats(ctx context.Context, req *coreapi.GetUserRequest) (*coreapi.UserStatsResponse, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id")
	}

	stats, err := h.logic.GetUserStats(ctx, ownerID)
	if err != nil {
		return nil, apperrors.ToGRPC(err)
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
