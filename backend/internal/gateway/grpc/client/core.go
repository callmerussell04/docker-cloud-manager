package grpcclient

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/domain/dto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
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

func (c *CoreClient) CreateContainer(ctx context.Context, ownerID string, createContainerDTO dto.CreateContainerDTO) (string, error) {
	var mounts []*coreapi.VolumeMount
	for _, m := range createContainerDTO.VolumeMounts {
		mounts = append(mounts, &coreapi.VolumeMount{
			VolumeId:   m.VolumeID,
			MountPath:  m.MountPath,
			IsReadonly: m.IsReadOnly,
		})
	}

	req := &coreapi.CreateContainerRequest{
		OwnerId:      ownerID,
		Name:         createContainerDTO.Name,
		ImageTag:     createContainerDTO.ImageTag,
		InternalPort: int32(createContainerDTO.InternalPort),
		EnvVars:      createContainerDTO.EnvVars,
		VolumeMounts: mounts,
		DomainPrefix: createContainerDTO.DomainPrefix,
	}

	resp, err := c.containerAPI.CreateContainer(ctx, req)
	if err != nil {
		return "", mapCoreError(err)
	}

	return resp.GetContainerId(), nil
}

func (c *CoreClient) GetUserContainers(ctx context.Context, ownerID string) ([]*coreapi.ContainerData, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.containerAPI.GetUserContainers(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return resp.GetContainers(), nil
}

func (c *CoreClient) ActionContainer(ctx context.Context, ownerID, containerID, action string) error {
	req := &coreapi.ContainerActionRequest{
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

func (c *CoreClient) ExposeContainer(ctx context.Context, ownerID, containerID, domainPrefix string, internalPort int) error {
	req := &coreapi.ExposeRequest{
		OwnerId:      ownerID,
		ContainerId:  containerID,
		DomainPrefix: domainPrefix,
		InternalPort: int32(internalPort),
	}
	_, err := c.containerAPI.ExposeContainer(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) CreateVolume(ctx context.Context, ownerID string, createVolumeDTO dto.CreateVolumeDTO) (string, error) {
	req := &coreapi.CreateVolumeRequest{
		OwnerId:    ownerID,
		Name:       createVolumeDTO.Name,
		Driver:     createVolumeDTO.Driver,
		DriverOpts: createVolumeDTO.DriverOpts,
	}
	resp, err := c.volumeAPI.CreateVolume(ctx, req)
	if err != nil {
		return "", mapCoreError(err)
	}
	return resp.GetVolumeId(), nil
}

func (c *CoreClient) DeleteVolume(ctx context.Context, ownerID, volumeID string) error {
	req := &coreapi.VolumeActionRequest{
		OwnerId:  ownerID,
		VolumeId: volumeID,
	}
	_, err := c.volumeAPI.DeleteVolume(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetUserVolumes(ctx context.Context, ownerID string) ([]*coreapi.VolumeData, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.volumeAPI.GetUserVolumes(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return resp.GetVolumes(), nil
}

func (c *CoreClient) GetUserImages(ctx context.Context, ownerID string) ([]*coreapi.ImageData, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.imageAPI.GetUserImages(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return resp.GetImages(), nil
}

func (c *CoreClient) DeleteImage(ctx context.Context, ownerID, imageID string) error {
	req := &coreapi.ImageActionRequest{
		OwnerId: ownerID,
		ImageId: imageID,
	}
	_, err := c.imageAPI.DeleteImage(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetUserBuilds(ctx context.Context, ownerID string) ([]*coreapi.BuildData, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.imageAPI.GetUserBuilds(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return resp.GetBuilds(), nil
}

func (c *CoreClient) DeleteBuild(ctx context.Context, ownerID, buildID string) error {
	req := &coreapi.BuildActionRequest{
		OwnerId: ownerID,
		BuildId: buildID,
	}
	_, err := c.imageAPI.DeleteBuild(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetUserProjects(ctx context.Context, ownerID string) ([]*coreapi.ProjectData, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.projectAPI.GetUserProjects(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return resp.GetProjects(), nil
}

func (c *CoreClient) DeleteProject(ctx context.Context, ownerID, projectID string) error {
	req := &coreapi.ProjectActionRequest{OwnerId: ownerID, ProjectId: projectID}
	_, err := c.projectAPI.DeleteProject(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) StopProject(ctx context.Context, ownerID, projectID string) error {
	req := &coreapi.ProjectActionRequest{OwnerId: ownerID, ProjectId: projectID}
	_, err := c.projectAPI.StopProject(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetAllContainers(ctx context.Context, page, limit int) (*coreapi.PaginatedContainerResponse, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.containerAPI.GetAllContainers(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return resp, nil
}

func (c *CoreClient) AdminActionContainer(ctx context.Context, containerID, action string) error {
	req := &coreapi.ContainerActionRequest{
		ContainerId: containerID,
		Action:      action,
	}

	_, err := c.containerAPI.AdminActionContainer(ctx, req)

	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetAllVolumes(ctx context.Context, page, limit int) (*coreapi.PaginatedVolumeResponse, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.volumeAPI.GetAllVolumes(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return resp, nil
}

func (c *CoreClient) AdminDeleteVolume(ctx context.Context, volumeID string) error {
	req := &coreapi.VolumeActionRequest{VolumeId: volumeID}
	_, err := c.volumeAPI.AdminDeleteVolume(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetAllImages(ctx context.Context, page, limit int) (*coreapi.PaginatedImageResponse, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.imageAPI.GetAllImages(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return resp, nil
}

func (c *CoreClient) AdminDeleteImage(ctx context.Context, imageID string) error {
	req := &coreapi.ImageActionRequest{ImageId: imageID}
	_, err := c.imageAPI.AdminDeleteImage(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetAllBuilds(ctx context.Context, page, limit int) (*coreapi.PaginatedBuildResponse, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.imageAPI.GetAllBuilds(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return resp, nil
}

func (c *CoreClient) AdminDeleteBuild(ctx context.Context, buildID string) error {
	req := &coreapi.BuildActionRequest{BuildId: buildID}
	_, err := c.imageAPI.AdminDeleteBuild(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetAllProjects(ctx context.Context, page, limit int) (*coreapi.PaginatedProjectResponse, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.projectAPI.GetAllProjects(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return resp, nil
}

func (c *CoreClient) AdminDeleteProject(ctx context.Context, projectID string) error {
	req := &coreapi.ProjectActionRequest{ProjectId: projectID}
	_, err := c.projectAPI.AdminDeleteProject(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) AdminStopProject(ctx context.Context, projectID string) error {
	req := &coreapi.ProjectActionRequest{ProjectId: projectID}
	_, err := c.projectAPI.AdminStopProject(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetSystemConfig(ctx context.Context) (*coreapi.SystemConfigData, error) {
	resp, err := c.systemAPI.GetConfig(ctx, &coreapi.Empty{})
	if err != nil {
		return nil, mapCoreError(err)
	}
	return resp, nil
}

func (c *CoreClient) UpdateSystemConfig(ctx context.Context, req *coreapi.SystemConfigData) error {
	_, err := c.systemAPI.UpdateConfig(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetContainerStats(ctx context.Context, ownerID, containerID string) (*coreapi.ContainerStatsResponse, error) {
	req := &coreapi.ContainerActionRequest{
		OwnerId:     ownerID,
		ContainerId: containerID,
	}
	resp, err := c.containerAPI.GetContainerStats(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return resp, nil
}

func (c *CoreClient) AdminGetContainerStats(ctx context.Context, containerID string) (*coreapi.ContainerStatsResponse, error) {
	req := &coreapi.ContainerActionRequest{
		ContainerId: containerID,
	}
	resp, err := c.containerAPI.AdminGetContainerStats(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return resp, nil
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
		return apperrors.ErrResourceExhausted
	case codes.InvalidArgument:
		return apperrors.ErrBadRequest
	case codes.Unauthenticated:
		return apperrors.ErrUnauthorized
	default:
		return apperrors.ErrInternal
	}
}

func (c *CoreClient) GetUserStats(ctx context.Context, ownerID string) (*coreapi.UserStatsResponse, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.statsAPI.GetUserStats(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return resp, nil
}
