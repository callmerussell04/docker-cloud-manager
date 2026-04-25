package grpcclient

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

func (c *CoreClient) GetSystemConfig(ctx context.Context) (dto.SystemConfigDTO, error) {
	resp, err := c.systemAPI.GetConfig(ctx, &coreapi.Empty{})
	if err != nil {
		return dto.SystemConfigDTO{}, apperrors.FromGRPC(err)
	}
	return systemConfigFromProto(resp), nil
}

func (c *CoreClient) UpdateSystemConfig(ctx context.Context, req dto.SystemConfigDTO) error {
	_, err := c.systemAPI.UpdateConfig(ctx, systemConfigToProto(req))
	if err != nil {
		return apperrors.FromGRPC(err)
	}
	return nil
}

func systemConfigFromProto(data *coreapi.SystemConfigData) dto.SystemConfigDTO {
	return dto.SystemConfigDTO{
		BaseDomain:                    data.GetBaseDomain(),
		DefaultMemoryReservationBytes: data.GetDefaultMemoryReservationBytes(),
		ReservedSystemMemoryBytes:     data.GetReservedSystemMemoryBytes(),
		OvercommitFactor:              data.GetOvercommitFactor(),
		MaxBurstMultiplier:            data.GetMaxBurstMultiplier(),
		DefaultCpuShares:              data.GetDefaultCpuShares(),
		HighLoadCpuShares:             data.GetHighLoadCpuShares(),
		HighLoadContainerCount:        int(data.GetHighLoadContainerCount()),
		ContainerStopTimeout:          int(data.GetContainerStopTimeout()),
		MaxLogSize:                    data.GetMaxLogSize(),
		MaxLogFiles:                   data.GetMaxLogFiles(),
		ContainerDiskQuota:            data.GetContainerDiskQuota(),
		MaxVolumesPerUser:             int(data.GetMaxVolumesPerUser()),
		MaxContainersPerUser:          int(data.GetMaxContainersPerUser()),
		RegistryApiUrl:                data.GetRegistryApiUrl(),
		RegistryPublicUrl:             data.GetRegistryPublicUrl(),
		ContainerTtlHours:             data.GetContainerTtlHours(),
	}
}

func systemConfigToProto(data dto.SystemConfigDTO) *coreapi.SystemConfigData {
	return &coreapi.SystemConfigData{
		BaseDomain:                    data.BaseDomain,
		DefaultMemoryReservationBytes: data.DefaultMemoryReservationBytes,
		ReservedSystemMemoryBytes:     data.ReservedSystemMemoryBytes,
		OvercommitFactor:              data.OvercommitFactor,
		MaxBurstMultiplier:            data.MaxBurstMultiplier,
		DefaultCpuShares:              data.DefaultCpuShares,
		HighLoadCpuShares:             data.HighLoadCpuShares,
		HighLoadContainerCount:        int32(data.HighLoadContainerCount),
		ContainerStopTimeout:          int32(data.ContainerStopTimeout),
		MaxLogSize:                    data.MaxLogSize,
		MaxLogFiles:                   data.MaxLogFiles,
		ContainerDiskQuota:            data.ContainerDiskQuota,
		MaxVolumesPerUser:             int32(data.MaxVolumesPerUser),
		MaxContainersPerUser:          int32(data.MaxContainersPerUser),
		RegistryApiUrl:                data.RegistryApiUrl,
		RegistryPublicUrl:             data.RegistryPublicUrl,
		ContainerTtlHours:             data.ContainerTtlHours,
	}
}
