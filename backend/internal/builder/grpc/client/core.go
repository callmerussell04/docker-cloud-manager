package grpcclient

import (
	"context"
	"time"

	"google.golang.org/grpc"

	coreapi "github.com/callmerussell04/docker-cloud-manager/backend/api/core"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/grpcerrors"
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
	ctx = accessscope.WithSystemScope(ctx)
	resp, err := c.systemAPI.GetBuilderRuntimeConfig(ctx, &coreapi.Empty{})
	if err != nil {
		return config.BuilderConfig{}, grpcerrors.FromGRPC(err)
	}
	return config.BuilderConfig{
		BuildMemoryBytes:          resp.GetBuildMemoryBytes(),
		BuildCPUQuota:             resp.GetBuildCpuQuota(),
		BuildCPUPeriod:            resp.GetBuildCpuPeriod(),
		BuildMemorySwapMultiplier: resp.GetBuildMemorySwapMultiplier(),
		BuildPidsLimit:            resp.GetBuildPidsLimit(),
		BuildNetworkName:          resp.GetBuildNetworkName(),
		KanikoImage:               resp.GetKanikoImage(),
		MaxBuildTime:              time.Duration(resp.GetMaxBuildTimeMinutes()) * time.Minute,
		MaxConcurrentBuilds:       int(resp.GetMaxConcurrentBuilds()),
		MaxUnpackedSizeBytes:      resp.GetMaxUnpackedSizeBytes(),
		MaxBuildLogSizeBytes:      resp.GetMaxBuildLogSizeBytes(),
		BuildCancelPollInterval:   time.Duration(resp.GetBuildCancelPollIntervalSeconds()) * time.Second,
	}, nil
}

func (c *CoreClient) StartBuildRecord(ctx context.Context, buildID string) (model.BuildClaim, bool, model.BuildStatus, error) {
	ctx = accessscope.WithSystemScope(ctx)
	resp, err := c.imageAPI.StartBuildRecord(ctx, &coreapi.BuildActionRequest{BuildId: buildID})
	if err != nil {
		return model.BuildClaim{}, false, "", grpcerrors.FromGRPC(err)
	}
	return model.BuildClaim{ImageID: resp.GetImageId(), RegistryUsername: resp.GetRegistryUsername(), RegistryPassword: resp.GetRegistryPassword(), Destination: resp.GetRegistryDestination()}, resp.GetStarted(), model.BuildStatus(resp.GetStatus()), nil
}

func (c *CoreClient) CompleteBuildRecord(ctx context.Context, buildID, imageID string, buildStatus model.BuildStatus, sizeMB int) error {
	ctx = accessscope.WithSystemScope(ctx)
	req := &coreapi.CompleteBuildRequest{
		BuildId: buildID,
		ImageId: imageID,
		Status:  string(buildStatus),
		SizeMb:  int32(sizeMB),
	}

	_, err := c.imageAPI.CompleteBuildRecord(ctx, req)
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}

	return nil
}

func (c *CoreClient) GetBuildStatus(ctx context.Context, buildID string) (model.BuildStatus, error) {
	ctx = accessscope.WithSystemScope(ctx)
	resp, err := c.imageAPI.GetBuildStatus(ctx, &coreapi.BuildActionRequest{BuildId: buildID})
	if err != nil {
		return "", grpcerrors.FromGRPC(err)
	}
	return model.BuildStatus(resp.GetStatus()), nil
}
