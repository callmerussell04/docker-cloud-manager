package client

import (
	"context"
	"time"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/telemetry/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
	"google.golang.org/grpc"
)

type CoreClient struct {
	containerAPI coreapi.ContainerAPIClient
	systemAPI    coreapi.SystemAPIClient
}

func NewCoreClient(cc *grpc.ClientConn) *CoreClient {
	return &CoreClient{
		containerAPI: coreapi.NewContainerAPIClient(cc),
		systemAPI:    coreapi.NewSystemAPIClient(cc),
	}
}

func (c *CoreClient) GetContainerTarget(ctx context.Context, containerID string) (model.ContainerTarget, error) {
	resp, err := c.containerAPI.GetContainerRuntimeTarget(ctx, &coreapi.ContainerRuntimeTargetRequest{
		ContainerId: containerID,
	})
	if err != nil {
		return model.ContainerTarget{}, grpcerrors.FromGRPC(err)
	}
	return model.ContainerTarget{
		ContainerID:      resp.GetContainerId(),
		DockerID:         resp.GetDockerId(),
		Status:           resp.GetStatus(),
		OwnerID:          resp.GetOwnerId(),
		DockerGeneration: int(resp.GetDockerGeneration()),
	}, nil
}

func (c *CoreClient) GetRuntimeConfig(ctx context.Context) (model.RuntimeConfig, error) {
	resp, err := c.systemAPI.GetTelemetryRuntimeConfig(ctx, &coreapi.Empty{})
	if err != nil {
		return model.RuntimeConfig{}, grpcerrors.FromGRPC(err)
	}
	return model.RuntimeConfig{
		MaxLogTailLines:            int(resp.GetTelemetryMaxLogTailLines()),
		MaxLogStreamsPerUser:       int(resp.GetTelemetryMaxLogStreamsPerUser()),
		MaxTerminalSessionsPerUser: int(resp.GetTelemetryMaxTerminalSessionsPerUser()),
		TerminalIdleTimeout:        time.Duration(resp.GetTelemetryTerminalIdleTimeoutSeconds()) * time.Second,
		TerminalMaxDuration:        time.Duration(resp.GetTelemetryTerminalMaxDurationSeconds()) * time.Second,
		AllowedExecCommands:        resp.GetTelemetryAllowedExecCommands(),
		MaxCommandArgs:             int(resp.GetTelemetryMaxCommandArgs()),
		MaxCommandArgBytes:         int(resp.GetTelemetryMaxCommandArgBytes()),
		WSReadLimitBytes:           resp.GetTelemetryWsReadLimitBytes(),
	}, nil
}
