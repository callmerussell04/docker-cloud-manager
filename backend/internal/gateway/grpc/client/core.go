package grpcclient

import (
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
