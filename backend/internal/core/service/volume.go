package service

import (
	"context"
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
)

type VolumeRepository interface {
	Save(ctx context.Context, vol model.Volume) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Volume, error)
	GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]model.Volume, error)
	Delete(ctx context.Context, id uuid.UUID) error
	CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error)
	IsVolumeInUse(ctx context.Context, volumeID uuid.UUID) (bool, error)
	GetAllPaginated(ctx context.Context, limit, offset int) ([]model.Volume, int, error)
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
	repo      VolumeRepository
	dockerAPI VolumeDockerAPI
	cfg       ConfigManager
	users     UserInfoProvider
	imageRepo volumeImageDiskRepository
}

func NewVolumeService(repo VolumeRepository, dockerAPI VolumeDockerAPI, cfg ConfigManager, deps ...any) *VolumeService {
	s := &VolumeService{
		repo:      repo,
		dockerAPI: dockerAPI,
		cfg:       cfg,
	}
	for _, dep := range deps {
		switch v := dep.(type) {
		case UserInfoProvider:
			s.users = v
		case volumeImageDiskRepository:
			s.imageRepo = v
		}
	}
	return s
}

func (s *VolumeService) ensureDiskQuotaAvailable(ctx context.Context, ownerID uuid.UUID) error {
	if s.users == nil || s.imageRepo == nil {
		return nil
	}
	user, err := s.users.GetUser(ctx, ownerID)
	if err != nil {
		return err
	}
	usedMB, err := s.imageRepo.GetUserUsedDiskSpace(ctx, ownerID)
	if err != nil {
		return err
	}
	if volumeRepo, ok := s.repo.(volumeDiskUsageRepository); ok {
		usedBytes, err := volumeRepo.GetUserUsedVolumeBytes(ctx, ownerID)
		if err != nil {
			return err
		}
		usedMB += bytesToMBRoundedUp(usedBytes)
	}
	if usedMB >= user.QuotaDiskMB {
		return apperrors.New(apperrors.ErrQuotaExceeded, "user disk quota exceeded")
	}
	return nil
}

func (s *VolumeService) Create(ctx context.Context, ownerID uuid.UUID, params model.VolumeCreateParams) (uuid.UUID, error) {
	if err := validation.ResourceName(params.Name); err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	if err := s.ensureDiskQuotaAvailable(ctx, ownerID); err != nil {
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
		Driver:     "local",
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

func (s *VolumeService) Delete(ctx context.Context, ownerID, volumeID uuid.UUID) error {
	vol, err := s.repo.GetByID(ctx, volumeID)
	if err != nil {
		return err
	}

	if vol.OwnerID != ownerID {
		return apperrors.ErrNotFound
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

func (s *VolumeService) GetByOwner(ctx context.Context, ownerID uuid.UUID) ([]model.Volume, error) {
	return s.repo.GetByOwnerID(ctx, ownerID)
}

func (s *VolumeService) GetAllPaginated(ctx context.Context, limit, offset int) ([]model.Volume, int, error) {
	return s.repo.GetAllPaginated(ctx, limit, offset)
}

func (s *VolumeService) AdminDelete(ctx context.Context, volumeID uuid.UUID) error {
	vol, err := s.repo.GetByID(ctx, volumeID)
	if err != nil {
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
