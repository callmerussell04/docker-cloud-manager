package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

type ContainerConfig struct {
	MaxContainersPerUser  int
	ReservedSystemMemory  int64
	OvercommitFactor      float64
	BaseMemoryReservation int64
	MaxBurstMemoryLimit   int64
	BaseDomain            string
}

type ContainerRepository interface {
	Save(ctx context.Context, c domain.Container) error
	GetByID(ctx context.Context, id uuid.UUID) (domain.Container, error)
	GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Container, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	Delete(ctx context.Context, id uuid.UUID) error
	CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error)
	CountRunning(ctx context.Context) (int, error)
	GetRunning(ctx context.Context) ([]domain.Container, error)
	UpdateResourcesConfig(ctx context.Context, id uuid.UUID, configJSON []byte) error
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
	UpdateContainerResources(ctx context.Context, dockerID string, memoryLimit, memoryReservation, cpuShares int64) error
}

type HostMetricsProvider interface {
	GetTotalMemory() (int64, error)
	GetFreeMemory() (int64, error)
}

type ContainerService struct {
	repo       ContainerRepository
	volumeRepo ContainerVolumeRepository
	dockerAPI  ContainerDockerAPI
	metrics    HostMetricsProvider
	config     ContainerConfig
}

func NewContainerService(
	repo ContainerRepository,
	volumeRepo ContainerVolumeRepository,
	dockerAPI ContainerDockerAPI,
	metrics HostMetricsProvider,
	config ContainerConfig,
) *ContainerService {
	return &ContainerService{
		repo:       repo,
		volumeRepo: volumeRepo,
		dockerAPI:  dockerAPI,
		metrics:    metrics,
		config:     config,
	}
}

func (s *ContainerService) Create(ctx context.Context, ownerID uuid.UUID, params domain.ContainerCreateParams) (uuid.UUID, error) {
	count, err := s.repo.CountByOwnerID(ctx, ownerID)
	if err != nil {
		return uuid.Nil, err
	}
	if count >= s.config.MaxContainersPerUser {
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

	envBytes, err := json.Marshal(params.EnvVars)
	if err != nil {
		return uuid.Nil, err
	}

	initialResConfig := map[string]interface{}{
		"memory_limit":       s.config.BaseMemoryReservation,
		"memory_reservation": s.config.BaseMemoryReservation,
		"cpu_shares":         512,
	}
	resBytes, err := json.Marshal(initialResConfig)
	if err != nil {
		return uuid.Nil, err
	}

	containerID := uuid.New()
	hostDomain := fmt.Sprintf("%s-%s.%s", params.Name, ownerID.String()[:8], s.config.BaseDomain)

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
		MemoryLimitBytes:  s.config.BaseMemoryReservation,
		MemoryReservation: s.config.BaseMemoryReservation,
		CPUShares:         512,
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

	if err := s.checkAdmissionCapacity(ctx); err != nil {
		return err
	}

	if err := s.dockerAPI.StartContainer(ctx, container.DockerID); err != nil {
		return err
	}

	err = s.repo.UpdateStatus(ctx, containerID, domain.ContainerStatusRunning)
	if err == nil {
		go s.RebalanceResources(context.Background())
	}

	return err
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

	err = s.repo.UpdateStatus(ctx, containerID, domain.ContainerStatusExited)
	if err == nil {
		go s.RebalanceResources(context.Background())
	}

	return err
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

func (s *ContainerService) checkAdmissionCapacity(ctx context.Context) error {
	runningCount, err := s.repo.CountRunning(ctx)
	if err != nil {
		return err
	}

	totalMem, err := s.metrics.GetTotalMemory()
	if err != nil {
		log.Printf("[AdmissionControl] Failed to get system memory: %v", err)
		return apperrors.ErrInternal
	}

	availablePool := float64(totalMem-s.config.ReservedSystemMemory) * s.config.OvercommitFactor

	if availablePool <= 0 {
		return apperrors.ErrResourceExhausted
	}

	projectedRequiredMem := int64(runningCount+1) * s.config.BaseMemoryReservation
	if projectedRequiredMem > int64(availablePool) {
		return apperrors.ErrResourceExhausted
	}

	return nil
}

func (s *ContainerService) RebalanceResources(ctx context.Context) {
	runningContainers, err := s.repo.GetRunning(ctx)
	if err != nil || len(runningContainers) == 0 {
		return
	}

	totalMem, err := s.metrics.GetTotalMemory()
	if err != nil {
		log.Printf("[Rebalancer] Failed to get total memory: %v. Aborting rebalance.", err)
		return
	}

	availableMem := totalMem - s.config.ReservedSystemMemory
	if availableMem <= 0 {
		availableMem = s.config.BaseMemoryReservation * int64(len(runningContainers))
	}

	fairShareMem := availableMem / int64(len(runningContainers))

	var newMemoryLimit int64
	if fairShareMem > s.config.MaxBurstMemoryLimit {
		newMemoryLimit = s.config.MaxBurstMemoryLimit
	} else if fairShareMem < s.config.BaseMemoryReservation {
		newMemoryLimit = s.config.BaseMemoryReservation
	} else {
		newMemoryLimit = fairShareMem
	}

	var cpuShares int64 = 1024
	if len(runningContainers) > 10 {
		cpuShares = 512
	}

	for _, c := range runningContainers {
		err := s.dockerAPI.UpdateContainerResources(ctx, c.DockerID, newMemoryLimit, s.config.BaseMemoryReservation, cpuShares)
		if err != nil {
			log.Printf("[Rebalancer] failed to update resources for container %s: %v", c.ID, err)
			continue
		}

		resConfig := map[string]interface{}{
			"memory_limit":       newMemoryLimit,
			"memory_reservation": s.config.BaseMemoryReservation,
			"cpu_shares":         cpuShares,
		}

		resBytes, err := json.Marshal(resConfig)
		if err == nil {
			s.repo.UpdateResourcesConfig(ctx, c.ID, resBytes)
		}
	}
}
