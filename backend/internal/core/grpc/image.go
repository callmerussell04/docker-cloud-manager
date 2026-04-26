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

type ImageLogic interface {
	GetByOwner(ctx context.Context, ownerID uuid.UUID) ([]model.Image, error)
	Delete(ctx context.Context, ownerID, imageID uuid.UUID) error
	GetAllPaginatedImages(ctx context.Context, limit, offset int) ([]model.Image, int, error)
	AdminDeleteImage(ctx context.Context, imageID uuid.UUID) error
}

type ImageHandler struct {
	coreapi.UnimplementedImageAPIServer
	imageLogic ImageLogic
	buildLogic BuildLogic
	users      UserDirectory
}

func RegisterImageAPI(gRPCServer *grpc.Server, imageLogic ImageLogic, buildLogic BuildLogic, users UserDirectory) {
	coreapi.RegisterImageAPIServer(gRPCServer, &ImageHandler{
		imageLogic: imageLogic,
		buildLogic: buildLogic,
		users:      users,
	})
}

func (h *ImageHandler) GetUserImages(ctx context.Context, req *coreapi.GetUserRequest) (*coreapi.ImageListResponse, error) {
	ownerID, err := uuid.Parse(req.GetOwnerId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner_id format")
	}

	images, err := h.imageLogic.GetByOwner(ctx, ownerID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	var pbImages []*coreapi.ImageData
	for _, img := range images {
		pbImages = append(pbImages, imageToProto(img, ""))
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

	err = h.imageLogic.Delete(ctx, ownerID, imageID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.Empty{}, nil
}

func (h *ImageHandler) GetAllImages(ctx context.Context, req *coreapi.PaginationRequest) (*coreapi.PaginatedImageResponse, error) {
	limit, offset := pagination(req)

	images, total, err := h.imageLogic.GetAllPaginatedImages(ctx, limit, offset)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	var pbImages []*coreapi.ImageData
	usernames := h.usernamesByImageOwner(ctx, images)
	for _, img := range images {
		pbImages = append(pbImages, imageToProto(img, usernames[img.OwnerID]))
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

	if err := h.imageLogic.AdminDeleteImage(ctx, imageID); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	return &coreapi.Empty{}, nil
}

func (h *ImageHandler) usernamesByImageOwner(ctx context.Context, images []model.Image) map[uuid.UUID]string {
	ids := make([]uuid.UUID, 0, len(images))
	seen := make(map[uuid.UUID]struct{}, len(images))
	for _, img := range images {
		if _, ok := seen[img.OwnerID]; ok {
			continue
		}
		seen[img.OwnerID] = struct{}{}
		ids = append(ids, img.OwnerID)
	}
	return usernamesByID(ctx, h.users, ids)
}

func imageToProto(img model.Image, ownerUsername string) *coreapi.ImageData {
	return &coreapi.ImageData{
		Id:            img.ID.String(),
		Tag:           img.Tag,
		SizeMb:        int32(img.SizeMB),
		IsCustom:      img.IsCustom,
		CreatedAt:     img.CreatedAt.Unix(),
		OwnerId:       img.OwnerID.String(),
		OwnerUsername: ownerUsername,
		Status:        img.Status,
		LastError:     stringValue(img.LastError),
	}
}
