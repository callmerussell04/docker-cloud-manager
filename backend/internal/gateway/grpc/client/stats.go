package grpcclient

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
)

func (c *CoreClient) GetUserStats(ctx context.Context) (model.UserStats, error) {
	resp, err := c.statsAPI.GetUserStats(ctx, &coreapi.Empty{})
	if err != nil {
		return model.UserStats{}, grpcerrors.FromGRPC(err)
	}
	return userStatsFromProto(resp), nil
}

func (c *CoreClient) GetSystemMonitoring(ctx context.Context) (model.SystemMonitoring, error) {
	resp, err := c.statsAPI.GetSystemMonitoring(ctx, &coreapi.Empty{})
	if err != nil {
		return model.SystemMonitoring{}, grpcerrors.FromGRPC(err)
	}
	return systemMonitoringFromProto(resp), nil
}

func userStatsFromProto(data *coreapi.UserStatsResponse) model.UserStats {
	return model.UserStats{
		ContainersTotal:   data.GetContainersTotal(),
		ContainersRunning: data.GetContainersRunning(),
		ContainersQuota:   data.GetContainersQuota(),
		RamUsedBytes:      data.GetRamUsedBytes(),
		RamQuotaBytes:     data.GetRamQuotaBytes(),
		DiskUsedMB:        data.GetDiskUsedMb(),
		DiskQuotaMB:       data.GetDiskQuotaMb(),
		VolumesTotal:      data.GetVolumesTotal(),
		VolumesQuota:      data.GetVolumesQuota(),
		ImagesTotal:       data.GetImagesTotal(),
		ProjectsTotal:     data.GetProjectsTotal(),
	}
}

func systemMonitoringFromProto(data *coreapi.SystemMonitoringResponse) model.SystemMonitoring {
	return model.SystemMonitoring{
		CPUPercent:             data.GetCpuPercent(),
		MemoryTotalBytes:       data.GetMemoryTotalBytes(),
		MemoryUsedBytes:        data.GetMemoryUsedBytes(),
		MemoryAvailableBytes:   data.GetMemoryAvailableBytes(),
		DiskTotalBytes:         data.GetDiskTotalBytes(),
		DiskUsedBytes:          data.GetDiskUsedBytes(),
		DiskFreeBytes:          data.GetDiskFreeBytes(),
		DCMReservedMemoryBytes: data.GetDcmReservedMemoryBytes(),
		DCMDiskUsedBytes:       data.GetDcmDiskUsedBytes(),
		ContainersTotal:        data.GetContainersTotal(),
		ContainersRunning:      data.GetContainersRunning(),
		ContainersStopped:      data.GetContainersStopped(),
		ContainersError:        data.GetContainersError(),
		ContainersMissing:      data.GetContainersMissing(),
		VolumesTotal:           data.GetVolumesTotal(),
		ImagesTotal:            data.GetImagesTotal(),
		BuildsTotal:            data.GetBuildsTotal(),
		ProjectsTotal:          data.GetProjectsTotal(),
		ObservedAt:             data.GetObservedAt(),
	}
}
