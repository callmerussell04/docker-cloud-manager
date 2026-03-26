package grpc

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type VolumeLogic interface {
	Create(ctx context.Context, ownerID uuid.UUID, params domain.VolumeCreateParams) (uuid.UUID, error)
	Delete(ctx context.Context, ownerID, volumeID uuid.UUID) error
	GetByOwner(ctx context.Context, ownerID uuid.UUID) ([]domain.Volume, error)
}

type VolumeHandler struct {
	coreapi.UnimplementedVolumeAPIServer
	logic VolumeLogic
}

func RegisterVolumeAPI(gRPCServer *grpc.Server, logic VolumeLogic) {
	coreapi.RegisterVolumeAPIServer(gRPCServer, &VolumeHandler{logic: logic})
}

func (h *VolumeHandler) CreateVolume(ctx context.Context, req *coreapi.CreateVolumeRequest) (*coreapi.CreateVolumeResponse, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume name is required")
	}

	params := domain.VolumeCreateParams{
		Name:       req.GetName(),
		Driver:     req.GetDriver(),
		DriverOpts: req.GetDriverOpts(),
	}

	volumeID, err := h.logic.Create(ctx, ownerID, params)
	if err != nil {
		if errors.Is(err, apperrors.ErrAlreadyExists) {
			return nil, status.Error(codes.AlreadyExists, "volume already exists")
		}
		return nil, status.Error(codes.Internal, "failed to create volume")
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
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "volume not found")
		}
		return nil, status.Error(codes.Internal, "failed to delete volume")
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
		return nil, status.Error(codes.Internal, "failed to retrieve volumes")
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
