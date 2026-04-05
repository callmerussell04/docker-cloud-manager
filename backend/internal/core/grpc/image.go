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

type ImageLogic interface {
	GetByOwner(ctx context.Context, ownerID uuid.UUID) ([]domain.Image, error)
	Delete(ctx context.Context, ownerID, imageID uuid.UUID) error
	InitBuild(ctx context.Context, ownerID uuid.UUID, tag, logFilePath string) (uuid.UUID, uuid.UUID, error)
	CompleteBuild(ctx context.Context, buildID, imageID uuid.UUID, status string, sizeMB int) error
}

type ImageHandler struct {
	coreapi.UnimplementedImageAPIServer
	logic ImageLogic
}

func RegisterImageAPI(gRPCServer *grpc.Server, logic ImageLogic) {
	coreapi.RegisterImageAPIServer(gRPCServer, &ImageHandler{logic: logic})
}

func (h *ImageHandler) GetUserImages(ctx context.Context, req *coreapi.GetUserRequest) (*coreapi.ImageListResponse, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	images, err := h.logic.GetByOwner(ctx, ownerID)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to retrieve images")
	}

	var pbImages []*coreapi.ImageData
	for _, img := range images {
		pbImages = append(pbImages, &coreapi.ImageData{
			Id:        img.ID.String(),
			Tag:       img.Tag,
			SizeMb:    int32(img.SizeMB),
			IsCustom:  img.IsCustom,
			CreatedAt: img.CreatedAt.Unix(),
		})
	}

	return &coreapi.ImageListResponse{
		Images: pbImages,
	}, nil
}

func (h *ImageHandler) DeleteImage(ctx context.Context, req *coreapi.ImageActionRequest) (*coreapi.Empty, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	imageID, err := uuid.Parse(req.GetImageId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid image_id format")
	}

	err = h.logic.Delete(ctx, ownerID, imageID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "image not found")
		}
		return nil, status.Error(codes.Internal, "failed to delete image")
	}

	return &coreapi.Empty{}, nil
}

func (h *ImageHandler) InitBuildRecord(ctx context.Context, req *coreapi.InitBuildRequest) (*coreapi.InitBuildResponse, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	if req.GetTag() == "" || req.GetLogFilePath() == "" {
		return nil, status.Error(codes.InvalidArgument, "tag and log file path are required")
	}

	imageID, buildID, err := h.logic.InitBuild(ctx, ownerID, req.GetTag(), req.GetLogFilePath())
	if err != nil {
		if errors.Is(err, apperrors.ErrAlreadyExists) {
			return nil, status.Error(codes.AlreadyExists, "image tag already exists")
		}
		return nil, status.Error(codes.Internal, "failed to initialize build record")
	}

	return &coreapi.InitBuildResponse{
		ImageId: imageID.String(),
		BuildId: buildID.String(),
	}, nil
}

func (h *ImageHandler) CompleteBuildRecord(ctx context.Context, req *coreapi.CompleteBuildRequest) (*coreapi.Empty, error) {
	buildID, err := uuid.Parse(req.GetBuildId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid build_id format")
	}

	imageID, err := uuid.Parse(req.GetImageId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid image_id format")
	}

	if req.GetStatus() != domain.BuildStatusSuccess && req.GetStatus() != domain.BuildStatusFailed {
		return nil, status.Error(codes.InvalidArgument, "invalid build status")
	}

	err = h.logic.CompleteBuild(ctx, buildID, imageID, req.GetStatus(), int(req.GetSizeMb()))
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "build or image not found")
		}
		return nil, status.Error(codes.Internal, "failed to complete build record")
	}

	return &coreapi.Empty{}, nil
}
