package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

// TODO: pull out into a config
const maxContainersPerUser = 10

type ContainerRepository interface {
	Save(ctx context.Context, c domain.Container) error
	GetByID(ctx context.Context, id uuid.UUID) (domain.Container, error)
	GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Container, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	Delete(ctx context.Context, id uuid.UUID) error
	CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error)
}

type ContainerVolumeRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (domain.Volume, error)
	SaveMounts(ctx context.Context, mounts []domain.VolumeMount) error
}

type ContainerDockerAPI interface {
	EnsureUserNetwork(ctx context.Context, networkName string) (string, error)
	PullImage(ctx context.Context, imageName string) error
	CreateContainer(ctx context.Context, params docker.CreateContainerParams) (string, error)
	StartContainer(ctx context.Context, dockerID string) error
	StopContainer(ctx context.Context, dockerID string, timeout int) error
	RemoveContainer(ctx context.Context, dockerID string, force bool) error
}

type HostMetricsProvider interface {
	GetFreeMemory() int64
}

type ContainerService struct {
	repo       ContainerRepository
	volumeRepo ContainerVolumeRepository
	dockerAPI  ContainerDockerAPI
	metrics    HostMetricsProvider
	baseDomain string
}

func NewContainerService(
	repo ContainerRepository,
	volumeRepo ContainerVolumeRepository,
	dockerAPI ContainerDockerAPI,
	metrics HostMetricsProvider,
	baseDomain string,
) *ContainerService {
	return &ContainerService{
		repo:       repo,
		volumeRepo: volumeRepo,
		dockerAPI:  dockerAPI,
		metrics:    metrics,
		baseDomain: baseDomain,
	}
}

func (s *ContainerService) Create(ctx context.Context, ownerID uuid.UUID, params domain.ContainerCreateParams) (uuid.UUID, error) {
	count, err := s.repo.CountByOwnerID(ctx, ownerID)
	if err != nil {
		return uuid.Nil, err
	}
	if count >= maxContainersPerUser {
		return uuid.Nil, apperrors.ErrAlreadyExists
	}

	networkName := fmt.Sprintf("net_user_%s", ownerID.String())
	_, err = s.dockerAPI.EnsureUserNetwork(ctx, networkName)
	if err != nil {
		return uuid.Nil, err
	}

	err = s.dockerAPI.PullImage(ctx, params.ImageTag)
	if err != nil {
		return uuid.Nil, err
	}

	memLimit, memRes, cpuShares := s.calculateDynamicQuotas()

	envBytes, err := json.Marshal(params.EnvVars)
	if err != nil {
		return uuid.Nil, err
	}

	resConfig := map[string]interface{}{
		"memory_limit":       memLimit,
		"memory_reservation": memRes,
		"cpu_shares":         cpuShares,
	}
	resBytes, err := json.Marshal(resConfig)
	if err != nil {
		return uuid.Nil, err
	}

	containerID := uuid.New()
	hostDomain := fmt.Sprintf("%s-%s.%s", params.Name, ownerID.String()[:8], s.baseDomain)

	var dockerMounts []docker.MountParam
	var dbMounts []domain.VolumeMount

	for _, vm := range params.VolumeMounts {
		vol, err := s.volumeRepo.GetByID(ctx, vm.VolumeID)
		if err != nil {
			return uuid.Nil, err
		}
		if vol.OwnerID != ownerID {
			return uuid.Nil, apperrors.ErrNotFound
		}

		dockerMounts = append(dockerMounts, docker.MountParam{
			VolumeName: vol.DockerName,
			Target:     vm.MountPath,
			ReadOnly:   vm.IsReadOnly,
		})

		dbMounts = append(dbMounts, domain.VolumeMount{
			ContainerID: containerID,
			VolumeID:    vol.ID,
			MountPath:   vm.MountPath,
			IsReadOnly:  vm.IsReadOnly,
		})
	}

	var envList []string
	for k, v := range params.EnvVars {
		envList = append(envList, fmt.Sprintf("%s=%s", k, v))
	}

	dockerParams := docker.CreateContainerParams{
		ContainerName:     fmt.Sprintf("usr_%s", containerID.String()[:12]),
		ImageName:         params.ImageTag,
		NetworkName:       networkName,
		Domain:            hostDomain,
		InternalPort:      params.InternalPort,
		EnvVars:           envList,
		MemoryLimitBytes:  memLimit,
		MemoryReservation: memRes,
		CPUShares:         cpuShares,
		VolumeMounts:      dockerMounts,
	}

	dockerID, err := s.dockerAPI.CreateContainer(ctx, dockerParams)
	if err != nil {
		return uuid.Nil, err
	}

	container := domain.Container{
		ID:              containerID,
		OwnerID:         ownerID,
		DockerID:        dockerID,
		Name:            params.Name,
		ImageTag:        params.ImageTag,
		InternalPort:    params.InternalPort,
		Status:          domain.ContainerStatusCreated,
		EnvVars:         envBytes,
		ResourcesConfig: resBytes,
	}

	if err := s.repo.Save(ctx, container); err != nil {
		s.dockerAPI.RemoveContainer(context.Background(), dockerID, true)
		return uuid.Nil, err
	}

	if len(dbMounts) > 0 {
		if err := s.volumeRepo.SaveMounts(ctx, dbMounts); err != nil {
			return uuid.Nil, err
		}
	}

	return container.ID, nil
}

func (s *ContainerService) Start(ctx context.Context, ownerID, containerID uuid.UUID) error {
	container, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return err
	}
	if container.OwnerID != ownerID {
		return apperrors.ErrNotFound
	}

	if err := s.dockerAPI.StartContainer(ctx, container.DockerID); err != nil {
		return err
	}

	return s.repo.UpdateStatus(ctx, containerID, domain.ContainerStatusRunning)
}

func (s *ContainerService) Stop(ctx context.Context, ownerID, containerID uuid.UUID) error {
	container, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return err
	}
	if container.OwnerID != ownerID {
		return apperrors.ErrNotFound
	}

	if err := s.dockerAPI.StopContainer(ctx, container.DockerID, 10); err != nil {
		return err
	}

	return s.repo.UpdateStatus(ctx, containerID, domain.ContainerStatusExited)
}

func (s *ContainerService) Delete(ctx context.Context, ownerID, containerID uuid.UUID) error {
	container, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return err
	}
	if container.OwnerID != ownerID {
		return apperrors.ErrNotFound
	}

	err = s.dockerAPI.RemoveContainer(ctx, container.DockerID, true)
	if err != nil {
		return err
	}

	return s.repo.Delete(ctx, containerID)
}

func (s *ContainerService) GetByOwner(ctx context.Context, ownerID uuid.UUID) ([]domain.Container, error) {
	return s.repo.GetByOwnerID(ctx, ownerID)
}

// TODO: change dynamic quota calculation algorithm
func (s *ContainerService) calculateDynamicQuotas() (int64, int64, int64) {
	freeMemory := s.metrics.GetFreeMemory()

	var memoryLimit int64
	var memoryReservation int64
	var cpuShares int64

	if freeMemory > 8*1024*1024*1024 {
		memoryLimit = 2048 * 1024 * 1024
		memoryReservation = 512 * 1024 * 1024
		cpuShares = 1024
	} else {
		memoryLimit = 512 * 1024 * 1024
		memoryReservation = 256 * 1024 * 1024
		cpuShares = 512
	}

	return memoryLimit, memoryReservation, cpuShares
}
