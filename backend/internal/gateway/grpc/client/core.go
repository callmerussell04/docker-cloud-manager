package client

import (
	"google.golang.org/grpc"

	coreapi "github.com/callmerussell04/docker-cloud-manager/backend/api/core"
)

type CoreClient struct {
	containerAPI coreapi.ContainerAPIClient
	imageAPI     coreapi.ImageAPIClient
	volumeAPI    coreapi.VolumeAPIClient
	projectAPI   coreapi.ProjectAPIClient
	systemAPI    coreapi.SystemAPIClient
	statsAPI     coreapi.StatsAPIClient
	reportAPI    coreapi.ReportAPIClient
	quotaAPI     coreapi.QuotaAPIClient
	workloadAPI  coreapi.WorkloadAPIClient
}

func NewCoreClient(cc *grpc.ClientConn) *CoreClient {
	return &CoreClient{
		containerAPI: coreapi.NewContainerAPIClient(cc),
		imageAPI:     coreapi.NewImageAPIClient(cc),
		volumeAPI:    coreapi.NewVolumeAPIClient(cc),
		projectAPI:   coreapi.NewProjectAPIClient(cc),
		systemAPI:    coreapi.NewSystemAPIClient(cc),
		statsAPI:     coreapi.NewStatsAPIClient(cc),
		reportAPI:    coreapi.NewReportAPIClient(cc),
		quotaAPI:     coreapi.NewQuotaAPIClient(cc),
		workloadAPI:  coreapi.NewWorkloadAPIClient(cc),
	}
}
