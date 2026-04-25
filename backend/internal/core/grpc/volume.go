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
	Create(ctx context.Context, ownerID uuid.UUID, params model.VolumeCreateParams) (uuid.UUID, error)
	Delete(ctx context.Context, ownerID, volumeID uuid.UUID) error
	GetByOwner(ctx context.Context, ownerID uuid.UUID) ([]model.Volume, error)
	GetAllPaginated(ctx context.Context, limit, offset int) ([]model.Volume, int, error)
	AdminDelete(ctx context.Context, volumeID uuid.UUID) error
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
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume name is required")
	}

	params := model.VolumeCreateParams{
		Name: req.GetName(),
	}

	volumeID, err := h.logic.Create(ctx, ownerID, params)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.CreateVolumeResponse{
		VolumeId: volumeID.String(),
	}, nil
}

func (h *VolumeHandler) DeleteVolume(ctx context.Context, req *coreapi.VolumeActionRequest) (*coreapi.Empty, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	volumeID, err := uuid.Parse(req.GetVolumeId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid volume_id format")
	}

	err = h.logic.Delete(ctx, ownerID, volumeID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.Empty{}, nil
}

func (h *VolumeHandler) GetUserVolumes(ctx context.Context, req *coreapi.GetUserRequest) (*coreapi.VolumeListResponse, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	volumes, err := h.logic.GetByOwner(ctx, ownerID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	var pbVolumes []*coreapi.VolumeData

	for _, v := range volumes {
		pbVolumes = append(pbVolumes, &coreapi.VolumeData{
			Id:         v.ID.String(),
			DockerName: v.DockerName,
			Driver:     v.Driver,
			CreatedAt:  v.CreatedAt.Unix(),
		})
	}

	return &coreapi.VolumeListResponse{
		Volumes: pbVolumes,
	}, nil
}

func (h *VolumeHandler) GetAllVolumes(ctx context.Context, req *coreapi.PaginationRequest) (*coreapi.PaginatedVolumeResponse, error) {
	limit := int(req.GetLimit())
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset := (int(req.GetPage()) - 1) * limit
	if offset < 0 {
		offset = 0
	}

	volumes, total, err := h.logic.GetAllPaginated(ctx, limit, offset)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	var pbVolumes []*coreapi.VolumeData
	usernames := h.usernamesByOwner(ctx, volumes)
	for _, v := range volumes {
		pbVolumes = append(pbVolumes, &coreapi.VolumeData{
			Id:            v.ID.String(),
			DockerName:    v.DockerName,
			Driver:        v.Driver,
			CreatedAt:     v.CreatedAt.Unix(),
			OwnerId:       v.OwnerID.String(),
			OwnerUsername: usernames[v.OwnerID],
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

func (h *VolumeHandler) AdminDeleteVolume(ctx context.Context, req *coreapi.VolumeActionRequest) (*coreapi.Empty, error) {
	volumeID, err := uuid.Parse(req.GetVolumeId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid volume_id format")
	}

	err = h.logic.AdminDelete(ctx, volumeID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.Empty{}, nil
}
