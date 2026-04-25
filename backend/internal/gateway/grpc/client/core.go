package grpcclient

import (
	"google.golang.org/grpc"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
)

type CoreClient struct {
	containerAPI coreapi.ContainerAPIClient
	imageAPI     coreapi.ImageAPIClient
	volumeAPI    coreapi.VolumeAPIClient
	projectAPI   coreapi.ProjectAPIClient
	systemAPI    coreapi.SystemAPIClient
	statsAPI     coreapi.StatsAPIClient
}

func NewCoreClient(cc *grpc.ClientConn) *CoreClient {
	return &CoreClient{
		containerAPI: coreapi.NewContainerAPIClient(cc),
		imageAPI:     coreapi.NewImageAPIClient(cc),
		volumeAPI:    coreapi.NewVolumeAPIClient(cc),
		projectAPI:   coreapi.NewProjectAPIClient(cc),
		systemAPI:    coreapi.NewSystemAPIClient(cc),
		statsAPI:     coreapi.NewStatsAPIClient(cc),
	}
}
