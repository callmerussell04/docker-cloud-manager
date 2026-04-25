package grpcclient

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
)

func (c *CoreClient) GetUserStats(ctx context.Context, ownerID string) (dto.UserStatsDTO, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.statsAPI.GetUserStats(ctx, req)
	if err != nil {
		return dto.UserStatsDTO{}, mapCoreError(err)
	}
	return userStatsFromProto(resp), nil
}

func userStatsFromProto(data *coreapi.UserStatsResponse) dto.UserStatsDTO {
	return dto.UserStatsDTO{
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
