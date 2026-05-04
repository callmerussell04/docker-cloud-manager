package grpcclient

import (
	"context"
	"time"

	"google.golang.org/grpc"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
)

type CoreClient struct {
	imageAPI  coreapi.ImageAPIClient
	systemAPI coreapi.SystemAPIClient
}

func NewCoreClient(cc *grpc.ClientConn) *CoreClient {
	return &CoreClient{
		imageAPI:  coreapi.NewImageAPIClient(cc),
		systemAPI: coreapi.NewSystemAPIClient(cc),
	}
}

func (c *CoreClient) GetBuilderConfig(ctx context.Context) (config.BuilderConfig, error) {
	resp, err := c.systemAPI.GetConfig(ctx, &coreapi.Empty{})
	if err != nil {
		return config.BuilderConfig{}, grpcerrors.FromGRPC(err)
	}
	return config.BuilderConfig{
		ImageBuildsEnabled:        resp.GetImageBuildsEnabled(),
		BuildMemoryBytes:          resp.GetBuildMemoryBytes(),
		BuildCPUQuota:             resp.GetBuildCpuQuota(),
		BuildCPUPeriod:            resp.GetBuildCpuPeriod(),
		BuildMemorySwapMultiplier: resp.GetBuildMemorySwapMultiplier(),
		BuildPidsLimit:            resp.GetBuildPidsLimit(),
		BuildNetworkName:          resp.GetBuildNetworkName(),
		KanikoImage:               resp.GetKanikoImage(),
		MaxBuildTime:              time.Duration(resp.GetMaxBuildTimeMinutes()) * time.Minute,
		MaxConcurrentBuilds:       int(resp.GetMaxConcurrentBuilds()),
		MaxUploadSizeBytes:        resp.GetMaxUploadSizeBytes(),
		MaxArchiveSizeBytes:       resp.GetMaxArchiveSizeBytes(),
		MaxUnpackedSizeBytes:      resp.GetMaxUnpackedSizeBytes(),
		MaxBuildLogSizeBytes:      resp.GetMaxBuildLogSizeBytes(),
		BuildCancelPollInterval:   time.Duration(resp.GetBuildCancelPollIntervalSeconds()) * time.Second,
	}, nil
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

func (c *CoreClient) GetBuildStatus(ctx context.Context, buildID string) (string, error) {
	resp, err := c.imageAPI.GetBuild(ctx, &coreapi.BuildActionRequest{BuildId: buildID})
	if err != nil {
		return "", grpcerrors.FromGRPC(err)
	}
	return resp.GetStatus(), nil
}
