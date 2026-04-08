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
	InitBuildRecord(ctx context.Context, ownerID uuid.UUID, tag string, logFilePath string) (uuid.UUID, uuid.UUID, error)
	CompleteBuildRecord(ctx context.Context, buildID, imageID uuid.UUID, status string, sizeMB int) error
	GetUserBuilds(ctx context.Context, ownerID uuid.UUID) ([]domain.Build, error)
	DeleteBuild(ctx context.Context, ownerID, buildID uuid.UUID) error
	GetAllPaginatedImages(ctx context.Context, limit, offset int) ([]domain.Image, int, error)
	AdminDeleteImage(ctx context.Context, imageID uuid.UUID) error
	GetAllPaginatedBuilds(ctx context.Context, limit, offset int) ([]domain.Build, int, error)
	AdminDeleteBuild(ctx context.Context, buildID uuid.UUID) error
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

	if req.GetTag() == "" {
		return nil, status.Error(codes.InvalidArgument, "image tag is required")
	}

	buildID, imageID, err := h.logic.InitBuildRecord(ctx, ownerID, req.GetTag(), req.GetLogFilePath())
	if err != nil {
		if errors.Is(err, apperrors.ErrQuotaExceeded) {
			return nil, status.Error(codes.ResourceExhausted, "disk quota exceeded")
		}
		return nil, status.Error(codes.Internal, "failed to initialize build record")
	}

	return &coreapi.InitBuildResponse{
		BuildId: buildID.String(),
		ImageId: imageID.String(),
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

	err = h.logic.CompleteBuildRecord(ctx, buildID, imageID, req.GetStatus(), int(req.GetSizeMb()))
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "build or image not found")
		}
		if errors.Is(err, apperrors.ErrQuotaExceeded) {
			return nil, status.Error(codes.ResourceExhausted, "image size exceeds user quota, image removed")
		}
		return nil, status.Error(codes.Internal, "failed to complete build record")
	}

	return &coreapi.Empty{}, nil
}

func (h *ImageHandler) GetUserBuilds(ctx context.Context, req *coreapi.GetUserRequest) (*coreapi.BuildListResponse, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	builds, err := h.logic.GetUserBuilds(ctx, ownerID)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to retrieve builds")
	}

	var pbBuilds []*coreapi.BuildData
	for _, b := range builds {
		var finishedAt int64
		if b.FinishedAt != nil {
			finishedAt = b.FinishedAt.Unix()
		}
		pbBuilds = append(pbBuilds, &coreapi.BuildData{
			Id:          b.ID.String(),
			ImageId:     b.ImageID.String(),
			Status:      b.Status,
			StartedAt:   b.StartedAt.Unix(),
			FinishedAt:  finishedAt,
			LogFilePath: b.LogFilePath,
		})
	}

	return &coreapi.BuildListResponse{
		Builds: pbBuilds,
	}, nil
}

func (h *ImageHandler) DeleteBuild(ctx context.Context, req *coreapi.BuildActionRequest) (*coreapi.Empty, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	buildID, err := uuid.Parse(req.GetBuildId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid build_id format")
	}

	err = h.logic.DeleteBuild(ctx, ownerID, buildID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "build not found")
		}
		return nil, status.Error(codes.Internal, "failed to delete build")
	}

	return &coreapi.Empty{}, nil
}

func (h *ImageHandler) GetAllImages(ctx context.Context, req *coreapi.PaginationRequest) (*coreapi.PaginatedImageResponse, error) {
	limit := int(req.GetLimit())
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset := (int(req.GetPage()) - 1) * limit
	if offset < 0 {
		offset = 0
	}

	images, total, err := h.logic.GetAllPaginatedImages(ctx, limit, offset)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get images")
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

	return &coreapi.PaginatedImageResponse{
		Images:     pbImages,
		TotalCount: int32(total),
	}, nil
}

func (h *ImageHandler) AdminDeleteImage(ctx context.Context, req *coreapi.ImageActionRequest) (*coreapi.Empty, error) {
	imageID, err := uuid.Parse(req.GetImageId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid image_id")
	}

	if err := h.logic.AdminDeleteImage(ctx, imageID); err != nil {
		return nil, status.Error(codes.Internal, "failed to delete image")
	}
	return &coreapi.Empty{}, nil
}

func (h *ImageHandler) GetAllBuilds(ctx context.Context, req *coreapi.PaginationRequest) (*coreapi.PaginatedBuildResponse, error) {
	limit := int(req.GetLimit())
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset := (int(req.GetPage()) - 1) * limit
	if offset < 0 {
		offset = 0
	}

	builds, total, err := h.logic.GetAllPaginatedBuilds(ctx, limit, offset)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get builds")
	}

	var pbBuilds []*coreapi.BuildData
	for _, b := range builds {
		var finishedAt int64
		if b.FinishedAt != nil {
			finishedAt = b.FinishedAt.Unix()
		}
		pbBuilds = append(pbBuilds, &coreapi.BuildData{
			Id:          b.ID.String(),
			ImageId:     b.ImageID.String(),
			Status:      b.Status,
			StartedAt:   b.StartedAt.Unix(),
			FinishedAt:  finishedAt,
			LogFilePath: b.LogFilePath,
		})
	}

	return &coreapi.PaginatedBuildResponse{
		Builds:     pbBuilds,
		TotalCount: int32(total),
	}, nil
}

func (h *ImageHandler) AdminDeleteBuild(ctx context.Context, req *coreapi.BuildActionRequest) (*coreapi.Empty, error) {
	buildID, err := uuid.Parse(req.GetBuildId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid build_id")
	}

	if err := h.logic.AdminDeleteBuild(ctx, buildID); err != nil {
		return nil, status.Error(codes.Internal, "failed to delete build")
	}
	return &coreapi.Empty{}, nil
}
