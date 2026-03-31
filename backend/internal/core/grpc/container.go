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

type ContainerLogic interface {
	Create(ctx context.Context, ownerID uuid.UUID, params domain.ContainerCreateParams) (uuid.UUID, error)
	Start(ctx context.Context, ownerID, containerID uuid.UUID) error
	Stop(ctx context.Context, ownerID, containerID uuid.UUID) error
	Delete(ctx context.Context, ownerID, containerID uuid.UUID) error
	GetByOwner(ctx context.Context, ownerID uuid.UUID) ([]domain.Container, error)
	Expose(ctx context.Context, ownerID, containerID uuid.UUID, domainName string) error
}

type ContainerHandler struct {
	coreapi.UnimplementedContainerAPIServer
	logic ContainerLogic
}

func RegisterContainerAPI(gRPCServer *grpc.Server, logic ContainerLogic) {
	coreapi.RegisterContainerAPIServer(gRPCServer, &ContainerHandler{logic: logic})
}

func (h *ContainerHandler) CreateContainer(ctx context.Context, req *coreapi.CreateContainerRequest) (*coreapi.CreateContainerResponse, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	if req.GetName() == "" || req.GetImageTag() == "" {
		return nil, status.Error(codes.InvalidArgument, "name and image_tag are required")
	}

	var mounts []domain.VolumeMountParams
	for _, m := range req.GetVolumeMounts() {
		volID, err := uuid.Parse(m.GetVolumeId())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid volume_id format")
		}
		mounts = append(mounts, domain.VolumeMountParams{
			VolumeID:   volID,
			MountPath:  m.GetMountPath(),
			IsReadOnly: m.GetIsReadonly(),
		})
	}

	params := domain.ContainerCreateParams{
		Name:         req.GetName(),
		ImageTag:     req.GetImageTag(),
		InternalPort: int(req.GetInternalPort()),
		EnvVars:      req.GetEnvVars(),
		VolumeMounts: mounts,
		Domain:       req.GetDomain(),
	}

	containerID, err := h.logic.Create(ctx, ownerID, params)
	if err != nil {
		if errors.Is(err, apperrors.ErrAlreadyExists) {
			return nil, status.Error(codes.ResourceExhausted, "container limit reached")
		}
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "related resource not found")
		}
		return nil, status.Error(codes.Internal, "failed to create container")
	}

	return &coreapi.CreateContainerResponse{
		ContainerId: containerID.String(),
	}, nil
}

func (h *ContainerHandler) StartContainer(ctx context.Context, req *coreapi.ContainerActionRequest) (*coreapi.Empty, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	containerID, err := uuid.Parse(req.GetContainerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid container_id format")
	}

	err = h.logic.Start(ctx, ownerID, containerID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "container not found")
		}
		return nil, status.Error(codes.Internal, "failed to start container")
	}

	return &coreapi.Empty{}, nil
}

func (h *ContainerHandler) StopContainer(ctx context.Context, req *coreapi.ContainerActionRequest) (*coreapi.Empty, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	containerID, err := uuid.Parse(req.GetContainerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid container_id format")
	}

	err = h.logic.Stop(ctx, ownerID, containerID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "container not found")
		}
		return nil, status.Error(codes.Internal, "failed to stop container")
	}

	return &coreapi.Empty{}, nil
}

func (h *ContainerHandler) DeleteContainer(ctx context.Context, req *coreapi.ContainerActionRequest) (*coreapi.Empty, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	containerID, err := uuid.Parse(req.GetContainerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid container_id format")
	}

	err = h.logic.Delete(ctx, ownerID, containerID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "container not found")
		}
		return nil, status.Error(codes.Internal, "failed to delete container")
	}

	return &coreapi.Empty{}, nil
}

func (h *ContainerHandler) GetUserContainers(ctx context.Context, req *coreapi.GetUserRequest) (*coreapi.ContainerListResponse, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	containers, err := h.logic.GetByOwner(ctx, ownerID)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to retrieve containers")
	}

	var pbContainers []*coreapi.ContainerData
	for _, c := range containers {
		pbContainers = append(pbContainers, &coreapi.ContainerData{
			Id:           c.ID.String(),
			DockerId:     c.DockerID,
			Name:         c.Name,
			ImageTag:     c.ImageTag,
			InternalPort: int32(c.InternalPort),
			Status:       c.Status,
			CreatedAt:    c.CreatedAt.Unix(),
		})
	}

	return &coreapi.ContainerListResponse{
		Containers: pbContainers,
	}, nil
}

func (h *ContainerHandler) ExposeContainer(ctx context.Context, req *coreapi.ExposeRequest) (*coreapi.Empty, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	containerID, err := uuid.Parse(req.GetContainerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid container_id format")
	}

	if req.GetDomain() == "" {
		return nil, status.Error(codes.InvalidArgument, "domain name is required")
	}

	err = h.logic.Expose(ctx, ownerID, containerID, req.GetDomain())
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "container not found")
		}
		return nil, status.Error(codes.Internal, "failed to expose container")
	}

	return &coreapi.Empty{}, nil
}
