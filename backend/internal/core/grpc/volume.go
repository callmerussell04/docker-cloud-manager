package grpc

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
)

type VolumeLogic interface {
	Create(ctx context.Context, params model.VolumeCreateParams) (uuid.UUID, error)
	Delete(ctx context.Context, volumeID uuid.UUID) error
	List(ctx context.Context, limit, offset int) ([]model.Volume, int, error)
}

type VolumeHandler struct {
	coreapi.UnimplementedVolumeAPIServer
	logic VolumeLogic
	users UserDirectory
}

func RegisterVolumeAPI(gRPCServer *grpc.Server, logic VolumeLogic, users UserDirectory) {
	coreapi.RegisterVolumeAPIServer(gRPCServer, &VolumeHandler{logic: logic, users: users})
}

func (h *VolumeHandler) CreateVolume(ctx context.Context, req *coreapi.CreateVolumeRequest) (*coreapi.CreateVolumeResponse, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume name is required")
	}

	params := model.VolumeCreateParams{
		Name: req.GetName(),
	}

	volumeID, err := h.logic.Create(ctx, params)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.CreateVolumeResponse{
		VolumeId: volumeID.String(),
	}, nil
}

func (h *VolumeHandler) DeleteVolume(ctx context.Context, req *coreapi.VolumeActionRequest) (*coreapi.Empty, error) {
	volumeID, err := uuid.Parse(req.GetVolumeId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid volume_id format")
	}

	err = h.logic.Delete(ctx, volumeID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.Empty{}, nil
}

func (h *VolumeHandler) ListVolumes(ctx context.Context, req *coreapi.PaginationRequest) (*coreapi.PaginatedVolumeResponse, error) {
	limit, offset := pagination(req)
	volumes, total, err := h.logic.List(ctx, limit, offset)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	var pbVolumes []*coreapi.VolumeData
	usernames := h.usernamesByOwner(ctx, volumes)
	for _, v := range volumes {
		pbVolumes = append(pbVolumes, &coreapi.VolumeData{
			Id:              v.ID.String(),
			DockerName:      v.DockerName,
			CreatedAt:       v.CreatedAt.Unix(),
			OwnerId:         v.OwnerID.String(),
			OwnerUsername:   usernames[v.OwnerID],
			Status:          v.Status,
			LastError:       stringValue(v.LastError),
			UsedBytes:       v.UsedBytes,
			UsageObservedAt: timePtrUnix(v.UsageObservedAt),
		})
	}

	return &coreapi.PaginatedVolumeResponse{
		Volumes:    pbVolumes,
		TotalCount: int32(total),
	}, nil
}

func (h *VolumeHandler) usernamesByOwner(ctx context.Context, volumes []model.Volume) map[uuid.UUID]string {
	ids := make([]uuid.UUID, 0, len(volumes))
	seen := make(map[uuid.UUID]struct{}, len(volumes))
	for _, v := range volumes {
		if _, ok := seen[v.OwnerID]; ok {
			continue
		}
		seen[v.OwnerID] = struct{}{}
		ids = append(ids, v.OwnerID)
	}
	return usernamesByID(ctx, h.users, ids)
}
