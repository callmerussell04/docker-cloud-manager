package grpcclient

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	core "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/dto"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type CoreClient struct {
	containerAPI core.ContainerAPIClient
	imageAPI     core.ImageAPIClient
	volumeAPI    core.VolumeAPIClient
}

func NewCoreClient(cc *grpc.ClientConn) *CoreClient {
	return &CoreClient{
		containerAPI: core.NewContainerAPIClient(cc),
		imageAPI:     core.NewImageAPIClient(cc),
		volumeAPI:    core.NewVolumeAPIClient(cc),
	}
}

func (c *CoreClient) CreateContainer(ctx context.Context, ownerID string, dto dto.CreateContainerDTO) (string, error) {
	var mounts []*core.VolumeMount
	for _, m := range dto.VolumeMounts {
		mounts = append(mounts, &core.VolumeMount{
			VolumeId:   m.VolumeID,
			MountPath:  m.MountPath,
			IsReadonly: m.IsReadOnly,
		})
	}

	req := &core.CreateContainerRequest{
		OwnerId:      ownerID,
		Name:         dto.Name,
		ImageTag:     dto.ImageTag,
		InternalPort: int32(dto.InternalPort),
		EnvVars:      dto.EnvVars,
		VolumeMounts: mounts,
		Domain:       dto.Domain,
	}

	resp, err := c.containerAPI.CreateContainer(ctx, req)
	if err != nil {
		return "", mapCoreError(err)
	}

	return resp.GetContainerId(), nil
}

func (c *CoreClient) GetUserContainers(ctx context.Context, ownerID string) ([]*core.ContainerData, error) {
	req := &core.GetUserRequest{OwnerId: ownerID}
	resp, err := c.containerAPI.GetUserContainers(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return resp.GetContainers(), nil
}

func (c *CoreClient) ActionContainer(ctx context.Context, ownerID, containerID, action string) error {
	req := &core.ContainerActionRequest{
		OwnerId:     ownerID,
		ContainerId: containerID,
	}

	var err error
	switch action {
	case "start":
		_, err = c.containerAPI.StartContainer(ctx, req)
	case "stop":
		_, err = c.containerAPI.StopContainer(ctx, req)
	case "delete":
		_, err = c.containerAPI.DeleteContainer(ctx, req)
	default:
		return apperrors.ErrBadRequest
	}

	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) ExposeContainer(ctx context.Context, ownerID, containerID, domainName string) error {
	req := &core.ExposeRequest{
		OwnerId:     ownerID,
		ContainerId: containerID,
		Domain:      domainName,
	}
	_, err := c.containerAPI.ExposeContainer(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) CreateVolume(ctx context.Context, ownerID string, dto dto.CreateVolumeDTO) (string, error) {
	req := &core.CreateVolumeRequest{
		OwnerId:    ownerID,
		Name:       dto.Name,
		Driver:     dto.Driver,
		DriverOpts: dto.DriverOpts,
	}
	resp, err := c.volumeAPI.CreateVolume(ctx, req)
	if err != nil {
		return "", mapCoreError(err)
	}
	return resp.GetVolumeId(), nil
}

func (c *CoreClient) DeleteVolume(ctx context.Context, ownerID, volumeID string) error {
	req := &core.VolumeActionRequest{
		OwnerId:  ownerID,
		VolumeId: volumeID,
	}
	_, err := c.volumeAPI.DeleteVolume(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetUserVolumes(ctx context.Context, ownerID string) ([]*core.VolumeData, error) {
	req := &core.GetUserRequest{OwnerId: ownerID}
	resp, err := c.volumeAPI.GetUserVolumes(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return resp.GetVolumes(), nil
}

func (c *CoreClient) GetUserImages(ctx context.Context, ownerID string) ([]*core.ImageData, error) {
	req := &core.GetUserRequest{OwnerId: ownerID}
	resp, err := c.imageAPI.GetUserImages(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return resp.GetImages(), nil
}

func (c *CoreClient) DeleteImage(ctx context.Context, ownerID, imageID string) error {
	req := &core.ImageActionRequest{
		OwnerId: ownerID,
		ImageId: imageID,
	}
	_, err := c.imageAPI.DeleteImage(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

// TODO: maybe pull out for sso grpc client
func mapCoreError(err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return apperrors.ErrInternal
	}
	switch st.Code() {
	case codes.NotFound:
		return apperrors.ErrNotFound
	case codes.AlreadyExists:
		return apperrors.ErrAlreadyExists
	case codes.ResourceExhausted:
		return apperrors.ErrInternal
	case codes.InvalidArgument:
		return apperrors.ErrBadRequest
	default:
		return apperrors.ErrInternal
	}
}
