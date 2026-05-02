package grpcclient

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
)

func (c *CoreClient) DeleteImage(ctx context.Context, imageID string) error {
	req := &coreapi.ImageActionRequest{
		ImageId: imageID,
	}
	_, err := c.imageAPI.DeleteImage(ctx, req)
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) GetAllImages(ctx context.Context, page, limit int) (model.PaginatedImages, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.imageAPI.ListImages(ctx, req)
	if err != nil {
		return model.PaginatedImages{}, grpcerrors.FromGRPC(err)
	}
	return model.PaginatedImages{
		Images:     imagesFromProto(resp.GetImages()),
		TotalCount: resp.GetTotalCount(),
	}, nil
}

func imagesFromProto(items []*coreapi.ImageData) []model.Image {
	result := make([]model.Image, 0, len(items))
	for _, item := range items {
		result = append(result, model.Image{
			ID:            item.GetId(),
			Tag:           item.GetTag(),
			SizeMB:        item.GetSizeMb(),
			Status:        item.GetStatus(),
			LastError:     item.GetLastError(),
			CreatedAt:     item.GetCreatedAt(),
			OwnerID:       item.GetOwnerId(),
			OwnerUsername: item.GetOwnerUsername(),
		})
	}
	return result
}
