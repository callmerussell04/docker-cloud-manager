package grpcclient

import (
	"context"

	"google.golang.org/grpc"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
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
		return "", "", grpcerrors.FromGRPC(err)
	}

	return resp.GetBuildId(), resp.GetImageId(), nil
}

func (c *CoreClient) CreateBuildJob(ctx context.Context, ownerID, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (string, string, error) {
	req := &coreapi.CreateBuildJobRequest{
		OwnerId:          ownerID,
		Tag:              tag,
		ArchiveObjectKey: archiveObjectKey,
		LogObjectKey:     logObjectKey,
		ContextDir:       contextDir,
		Dockerfile:       dockerfile,
		BuildArgs:        buildArgs,
		RequestId:        requestID,
	}

	resp, err := c.imageAPI.CreateBuildJob(ctx, req)
	if err != nil {
		return "", "", grpcerrors.FromGRPC(err)
	}
	return resp.GetBuildId(), resp.GetImageId(), nil
}

func (c *CoreClient) StartBuildRecord(ctx context.Context, buildID string) (string, bool, string, error) {
	resp, err := c.imageAPI.StartBuildRecord(ctx, &coreapi.BuildActionRequest{BuildId: buildID})
	if err != nil {
		return "", false, "", grpcerrors.FromGRPC(err)
	}
	return resp.GetImageId(), resp.GetStarted(), resp.GetStatus(), nil
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
		return grpcerrors.FromGRPC(err)
	}

	return nil
}

func (c *CoreClient) CancelBuildRecord(ctx context.Context, buildID string) error {
	_, err := c.imageAPI.CancelBuildRecord(ctx, &coreapi.BuildActionRequest{BuildId: buildID})
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) GetBuildLogObjectKey(ctx context.Context, buildID string) (string, error) {
	resp, err := c.imageAPI.GetBuild(ctx, &coreapi.BuildActionRequest{BuildId: buildID})
	if err != nil {
		return "", grpcerrors.FromGRPC(err)
	}
	return resp.GetLogFilePath(), nil
}
