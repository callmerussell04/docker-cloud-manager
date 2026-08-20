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
	List(ctx context.Context, limit, offset int) ([]model.Image, int, error)
	Delete(ctx context.Context, imageID uuid.UUID) error
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

func (h *ImageHandler) DeleteImage(ctx context.Context, req *coreapi.ImageActionRequest) (*coreapi.Empty, error) {
	if err := requireUserOrAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	imageID, err := uuid.Parse(req.GetImageId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid image_id format")
	}

	err = h.imageLogic.Delete(ctx, imageID)
	if err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}

	return &coreapi.Empty{}, nil
}

func (h *ImageHandler) ListImages(ctx context.Context, req *coreapi.PaginationRequest) (*coreapi.PaginatedImageResponse, error) {
	if err := requireUserOrAdminScope(ctx); err != nil {
		return nil, grpcerrors.ToGRPC(err)
	}
	limit, offset := pagination(req)

	images, total, err := h.imageLogic.List(ctx, limit, offset)
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
		CreatedAt:     img.CreatedAt.Unix(),
		OwnerId:       img.OwnerID.String(),
		OwnerUsername: ownerUsername,
		Status:        img.Status,
		LastError:     stringValue(img.LastError),
	}
}
