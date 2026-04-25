package grpcclient

import (
	"context"

	"google.golang.org/grpc"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type CoreClient struct {
	imageAPI coreapi.ImageAPIClient
}

func NewCoreClient(cc *grpc.ClientConn) *CoreClient {
	return &CoreClient{
		imageAPI: coreapi.NewImageAPIClient(cc),
	}
}

func (c *CoreClient) InitBuildRecord(ctx context.Context, ownerID, tag, logFilePath string) (string, string, error) {
	req := &coreapi.InitBuildRequest{
		OwnerId:     ownerID,
		Tag:         tag,
		LogFilePath: logFilePath,
	}

	resp, err := c.imageAPI.InitBuildRecord(ctx, req)
	if err != nil {
		return "", "", apperrors.FromGRPC(err)
	}

	return resp.GetBuildId(), resp.GetImageId(), nil
}

func (c *CoreClient) CompleteBuildRecord(ctx context.Context, buildID, imageID, buildStatus string, sizeMB int) error {
	req := &coreapi.CompleteBuildRequest{
		BuildId: buildID,
		ImageId: imageID,
		Status:  buildStatus,
		SizeMb:  int32(sizeMB),
	}

	_, err := c.imageAPI.CompleteBuildRecord(ctx, req)
	if err != nil {
		return apperrors.FromGRPC(err)
	}

	return nil
}
