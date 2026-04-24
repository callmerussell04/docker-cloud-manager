package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/validation"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

type VolumeRepository interface {
	Save(ctx context.Context, vol domain.Volume) error
	GetByID(ctx context.Context, id uuid.UUID) (domain.Volume, error)
	GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Volume, error)
	Delete(ctx context.Context, id uuid.UUID) error
	CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error)
	IsVolumeInUse(ctx context.Context, volumeID uuid.UUID) (bool, error)
	GetAllPaginated(ctx context.Context, limit, offset int) ([]domain.Volume, int, error)
}

type VolumeDockerAPI interface {
	CreateVolume(ctx context.Context, params docker.CreateVolumeParams) (string, error)
	RemoveVolume(ctx context.Context, volumeName string, force bool) error
}

type VolumeService struct {
	repo      VolumeRepository
	dockerAPI VolumeDockerAPI
	cfg       ConfigManager
}

func NewVolumeService(repo VolumeRepository, dockerAPI VolumeDockerAPI, cfg ConfigManager) *VolumeService {
	return &VolumeService{
		repo:      repo,
		dockerAPI: dockerAPI,
		cfg:       cfg,
	}
}

func (s *VolumeService) Create(ctx context.Context, ownerID uuid.UUID, params domain.VolumeCreateParams) (uuid.UUID, error) {
	if err := validation.ResourceName(params.Name); err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	if params.Driver != "" && params.Driver != "local" {
		return uuid.Nil, fmt.Errorf("%w: only local volumes are allowed", apperrors.ErrBadRequest)
	}
	if len(params.DriverOpts) > 0 {
		return uuid.Nil, fmt.Errorf("%w: volume driver options are not allowed", apperrors.ErrBadRequest)
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

	optsBytes, err := json.Marshal(params.DriverOpts)
	if err != nil {
		return uuid.Nil, err
	}

	dockerParams := docker.CreateVolumeParams{
		VolumeName: dockerName,
		Driver:     params.Driver,
		DriverOpts: params.DriverOpts,
	}

	_, err = s.dockerAPI.CreateVolume(ctx, dockerParams)
	if err != nil {
		return uuid.Nil, err
	}

	vol := domain.Volume{
		ID:         volID,
		OwnerID:    ownerID,
		DockerName: dockerName,
		Driver:     params.Driver,
		DriverOpts: optsBytes,
	}

	if err := s.repo.Save(ctx, vol); err != nil {
		s.dockerAPI.RemoveVolume(context.Background(), dockerName, true)
		return uuid.Nil, err
	}

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
		return fmt.Errorf("conflict: unable to remove volume, it is currently in use by a container")
	}

	if err := s.dockerAPI.RemoveVolume(ctx, vol.DockerName, false); err != nil {
		return err
	}

	return s.repo.Delete(ctx, volumeID)
}

func (s *VolumeService) GetByOwner(ctx context.Context, ownerID uuid.UUID) ([]domain.Volume, error) {
	return s.repo.GetByOwnerID(ctx, ownerID)
}

func (s *VolumeService) GetAllPaginated(ctx context.Context, limit, offset int) ([]domain.Volume, int, error) {
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
		return fmt.Errorf("conflict: unable to remove volume, it is currently in use")
	}

	if err := s.dockerAPI.RemoveVolume(ctx, vol.DockerName, false); err != nil {
		return err
	}

	return s.repo.Delete(ctx, volumeID)
}
