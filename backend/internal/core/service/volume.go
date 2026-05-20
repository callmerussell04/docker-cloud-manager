package service

import (
	"context"
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
)

type VolumeRepository interface {
	Save(ctx context.Context, vol model.Volume) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Volume, error)
	Delete(ctx context.Context, id uuid.UUID) error
	CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error)
	IsVolumeInUse(ctx context.Context, volumeID uuid.UUID) (bool, error)
	List(ctx context.Context, opts model.ListOptions) ([]model.Volume, int, error)
}

type volumeDiskUsageRepository interface {
	GetUserUsedVolumeBytes(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

type volumeImageDiskRepository interface {
	GetUserUsedDiskSpace(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

type volumeStateRepository interface {
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error
}

type VolumeDockerAPI interface {
	CreateVolume(ctx context.Context, params model.VolumeRuntimeSpec) (string, error)
	RemoveVolume(ctx context.Context, volumeName string, force bool) error
}

type VolumeService struct {
	repo         VolumeRepository
	dockerAPI    VolumeDockerAPI
	cfg          ConfigManager
	users        UserInfoProvider
	imageRepo    volumeImageDiskRepository
	diskMetrics  HostDiskMetricsProvider
	hostDiskPath string
}

type VolumeServiceDeps struct {
	Users        UserInfoProvider
	ImageRepo    volumeImageDiskRepository
	DiskMetrics  HostDiskMetricsProvider
	HostDiskPath string
}

func NewVolumeService(repo VolumeRepository, dockerAPI VolumeDockerAPI, cfg ConfigManager, deps VolumeServiceDeps) *VolumeService {
	return &VolumeService{
		repo:         repo,
		dockerAPI:    dockerAPI,
		cfg:          cfg,
		users:        deps.Users,
		imageRepo:    deps.ImageRepo,
		diskMetrics:  deps.DiskMetrics,
		hostDiskPath: deps.HostDiskPath,
	}
}

func (s *VolumeService) ensureDiskQuotaAvailable(ctx context.Context, ownerID uuid.UUID) error {
	volumeRepo, _ := s.repo.(volumeDiskUsageRepository)
	return ensureDiskQuotaAvailable(ctx, ownerID, s.users, s.imageRepo, volumeRepo)
}

func (s *VolumeService) ensureHostDiskFloor() error {
	if s.cfg == nil {
		return nil
	}
	return ensureHostDiskFloor(s.diskMetrics, s.hostDiskPath, s.cfg.Get().HostMinFreeDiskBytes)
}

func (s *VolumeService) Create(ctx context.Context, params model.VolumeCreateParams) (uuid.UUID, error) {
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if err := validation.ResourceName(params.Name); err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	if err := s.ensureDiskQuotaAvailable(ctx, ownerID); err != nil {
		return uuid.Nil, err
	}
	if err := s.ensureHostDiskFloor(); err != nil {
		return uuid.Nil, err
	}

	// Проверка лимита на количество томов
	count, err := s.repo.CountByOwnerID(ctx, ownerID)
	if err != nil {
		return uuid.Nil, err
	}
	if count >= s.cfg.Get().MaxVolumesPerUser {
		return uuid.Nil, apperrors.ErrLimitExceeded
	}

	volID := uuid.New()
	dockerName := fmt.Sprintf("vol_%s_%s", ownerID.String()[:8], params.Name)

	vol := model.Volume{
		ID:         volID,
		OwnerID:    ownerID,
		ProjectID:  params.ProjectID,
		DockerName: dockerName,
		Status:     model.VolumeStatusCreating,
	}

	if err := s.repo.Save(ctx, vol); err != nil {
		return uuid.Nil, err
	}

	dockerParams := model.VolumeRuntimeSpec{
		VolumeName: dockerName,
		VolumeID:   volID.String(),
		OwnerID:    ownerID.String(),
	}
	if params.ProjectID != nil {
		dockerParams.ProjectID = params.ProjectID.String()
	}

	_, err = s.dockerAPI.CreateVolume(ctx, dockerParams)
	if err != nil {
		s.markVolumeError(ctx, volID, model.VolumeStatusError, err)
		return uuid.Nil, err
	}
	s.setVolumeStatus(ctx, volID, model.VolumeStatusAvailable)

	return volID, nil
}

func (s *VolumeService) Delete(ctx context.Context, volumeID uuid.UUID) error {
	vol, err := s.repo.GetByID(ctx, volumeID)
	if err != nil {
		return err
	}

	if err := accessscope.RequireOwnerAccess(ctx, vol.OwnerID); err != nil {
		return err
	}

	inUse, err := s.repo.IsVolumeInUse(ctx, volumeID)
	if err != nil {
		return err
	}
	if inUse {
		return apperrors.New(apperrors.ErrResourceInUse, "volume is currently used by a container")
	}
	s.setVolumeStatus(ctx, volumeID, model.VolumeStatusDeleting)

	if err := s.dockerAPI.RemoveVolume(ctx, vol.DockerName, false); err != nil && !cerrdefs.IsNotFound(err) {
		s.markVolumeError(ctx, volumeID, model.VolumeStatusError, err)
		return err
	}

	return s.repo.Delete(ctx, volumeID)
}

func (s *VolumeService) List(ctx context.Context, limit, offset int) ([]model.Volume, int, error) {
	scope, err := accessscope.RequireScope(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.List(ctx, model.ListOptions{
		OwnerID: scope.OwnerFilter(),
		Limit:   limit,
		Offset:  offset,
	})
}

func (s *VolumeService) setVolumeStatus(ctx context.Context, volumeID uuid.UUID, status string) {
	stateRepo, ok := s.repo.(volumeStateRepository)
	if !ok {
		return
	}
	_ = stateRepo.UpdateStatus(ctx, volumeID, status)
}

func (s *VolumeService) markVolumeError(ctx context.Context, volumeID uuid.UUID, status string, cause error) {
	stateRepo, ok := s.repo.(volumeStateRepository)
	if !ok {
		return
	}
	_ = stateRepo.MarkStatusError(ctx, volumeID, status, cause)
}
