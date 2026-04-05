package grpcclient

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
		st, ok := status.FromError(err)
		if ok {
			switch st.Code() {
			case codes.AlreadyExists:
				return "", "", apperrors.ErrAlreadyExists
			case codes.ResourceExhausted:
				return "", "", apperrors.ErrResourceExhausted
			}
		}
		return "", "", apperrors.ErrInternal
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
		st, ok := status.FromError(err)
		if ok {
			if st.Code() == codes.NotFound {
				return apperrors.ErrNotFound
			}
		}
		return apperrors.ErrInternal
	}

	return nil
}
