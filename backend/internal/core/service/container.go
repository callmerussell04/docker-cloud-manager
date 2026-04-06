package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/google/uuid"
)

type ContainerConfig struct {
	BaseDomain               string
	DefaultMemoryReservation int64
	ReservedSystemMemory     int64
	OvercommitFactor         float64
	MaxBurstMultiplier       int64
	DefaultCPUShares         int64
	HighLoadCPUShares        int64
	HighLoadContainerCount   int
	ContainerStopTimeout     int
	MaxLogSize               string
	MaxLogFiles              string
	ContainerDiskQuota       string
	MaxVolumesPerUser        int
	MaxContainersPerUser     int
	RegistryURL              string
}

type ContainerRepository interface {
	Save(ctx context.Context, c domain.Container) error
	GetByID(ctx context.Context, id uuid.UUID) (domain.Container, error)
	GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Container, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	UpdateDockerID(ctx context.Context, id uuid.UUID, dockerID string) error
	UpdateRouting(ctx context.Context, id uuid.UUID, domainPrefix string, internalPort int) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetUserReservedMemory(ctx context.Context, ownerID uuid.UUID) (int64, error)
	GetUserRAMQuota(ctx context.Context, ownerID uuid.UUID) (int64, error)
	GetRunning(ctx context.Context) ([]domain.Container, error)
	CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error)
	GetTotalSystemReservedMemory(ctx context.Context) (int64, error)
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
	InspectContainer(ctx context.Context, dockerID string) (*container.InspectResponse, error)
	ImageExists(ctx context.Context, imageTag string) (bool, error)
}

type HostMetricsProvider interface {
	GetTotalMemory() (int64, error)
	GetFreeMemory() (int64, error)
}

type ContainerImageRepository interface {
	GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Image, error)
}

type ContainerService struct {
	repo       ContainerRepository
	volumeRepo ContainerVolumeRepository
	imageRepo  ContainerImageRepository
	dockerAPI  ContainerDockerAPI
	metrics    HostMetricsProvider
	config     ContainerConfig
}

func NewContainerService(
	repo ContainerRepository,
	volumeRepo ContainerVolumeRepository,
	imageRepo ContainerImageRepository,
	dockerAPI ContainerDockerAPI,
	metrics HostMetricsProvider,
	config ContainerConfig,
) *ContainerService {
	return &ContainerService{
		repo:       repo,
		volumeRepo: volumeRepo,
		imageRepo:  imageRepo,
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
		return uuid.Nil, apperrors.ErrLimitExceeded
	}

	var fullDomain string
	if params.DomainPrefix != "" {
		if params.InternalPort <= 0 {
			return uuid.Nil, apperrors.ErrBadRequest
		}
		fullDomain = fmt.Sprintf("%s.%s", params.DomainPrefix, s.config.BaseDomain)
	}

	// 1. Определение запрашиваемой памяти (Гарантии)
	reqMem := params.RequestedMemoryMB * 1024 * 1024
	if reqMem <= 0 {
		reqMem = s.config.DefaultMemoryReservation
	}

	// 2. Admission Control: Проверка квоты пользователя
	if err := s.checkUserQuota(ctx, ownerID, reqMem); err != nil {
		return uuid.Nil, err
	}

	// 3. Admission Control: Проверка свободных ресурсов хоста (Защита сервера)
	if err := s.checkHostCapacity(reqMem); err != nil {
		return uuid.Nil, err
	}

	// 4. Изоляция сети
	networkName := fmt.Sprintf("net_user_%s", ownerID.String())
	_, err = s.dockerAPI.EnsureUserNetwork(ctx, networkName)
	if err != nil {
		return uuid.Nil, err
	}

	isCustom := false
	userImages, err := s.imageRepo.GetByOwnerID(ctx, ownerID)
	if err == nil {
		for _, img := range userImages {
			if img.Tag == params.ImageTag && img.IsCustom {
				isCustom = true
				break
			}
		}
	}

	actualImageTag := params.ImageTag
	if isCustom {
		// Формируем тег для локального Registry
		repoName := strings.ToLower(fmt.Sprintf("%s_%s", ownerID.String(), params.ImageTag))
		actualImageTag = fmt.Sprintf("%s/%s:latest", s.config.RegistryURL, repoName)
	}

	// Если образ кастомный — ПУЛЛИМ ВСЕГДА (вдруг пользователь пересобрал его)
	// Если публичный — пуллим только если его нет на хосте
	if isCustom {
		err = s.dockerAPI.PullImage(ctx, actualImageTag)
		if err != nil {
			return uuid.Nil, fmt.Errorf("failed to pull custom image: %v", err)
		}
	} else {
		imageExists, err := s.dockerAPI.ImageExists(ctx, actualImageTag)
		if err != nil {
			return uuid.Nil, err
		}
		if !imageExists {
			err = s.dockerAPI.PullImage(ctx, actualImageTag)
			if err != nil {
				return uuid.Nil, err
			}
		}
	}

	envBytes, err := json.Marshal(params.EnvVars)
	if err != nil {
		return uuid.Nil, err
	}

	containerID := uuid.New()

	// Подготовка томов
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

	// 5. Конфигурация Docker. Изначально ставим жесткий лимит равным мягкому.
	// Ребалансировщик потом его увеличит (Burst).
	dockerParams := docker.CreateContainerParams{
		ContainerName:     fmt.Sprintf("usr_%s", containerID.String()[:12]),
		ImageName:         actualImageTag,
		NetworkName:       networkName,
		Domain:            fullDomain,
		InternalPort:      params.InternalPort,
		EnvVars:           envList,
		MemoryLimitBytes:  reqMem,                    // Стартовый жесткий лимит
		MemoryReservation: reqMem,                    // Гарантия (Soft limit)
		CPUShares:         s.config.DefaultCPUShares, // Базовый приоритет
		VolumeMounts:      dockerMounts,
		MaxLogSize:        s.config.MaxLogSize,
		MaxLogFiles:       s.config.MaxLogFiles,
		StorageQuota:      s.config.ContainerDiskQuota,
	}

	dockerID, err := s.dockerAPI.CreateContainer(ctx, dockerParams)
	if err != nil {
		return uuid.Nil, err
	}

	c := domain.Container{
		ID:                    containerID,
		OwnerID:               ownerID,
		DockerID:              dockerID,
		Name:                  params.Name,
		ImageTag:              params.ImageTag,
		InternalPort:          params.InternalPort,
		DomainPrefix:          params.DomainPrefix,
		Status:                domain.ContainerStatusCreated,
		EnvVars:               envBytes,
		BaseMemoryReservation: reqMem,
	}

	if err := s.repo.Save(ctx, c); err != nil {
		s.dockerAPI.RemoveContainer(context.Background(), dockerID, true)
		return uuid.Nil, err
	}

	if len(dbMounts) > 0 {
		if err := s.volumeRepo.SaveMounts(ctx, dbMounts); err != nil {
			return uuid.Nil, err
		}
	}

	return containerID, nil
}

func (s *ContainerService) Expose(ctx context.Context, ownerID, containerID uuid.UUID, domainPrefix string, internalPort int) error {
	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return err
	}
	if c.OwnerID != ownerID {
		return apperrors.ErrNotFound
	}

	if domainPrefix == "" || internalPort <= 0 {
		return apperrors.ErrBadRequest
	}

	inspect, err := s.dockerAPI.InspectContainer(ctx, c.DockerID)
	if err != nil {
		return err
	}

	err = s.dockerAPI.StopContainer(ctx, c.DockerID, s.config.ContainerStopTimeout)
	if err != nil {
		return err
	}

	err = s.dockerAPI.RemoveContainer(ctx, c.DockerID, false)
	if err != nil {
		return err
	}

	var envList []string
	envList = append(envList, inspect.Config.Env...)

	var dockerMounts []docker.MountParam
	for _, m := range inspect.Mounts {
		if m.Type == mount.TypeVolume {
			dockerMounts = append(dockerMounts, docker.MountParam{
				VolumeName: m.Name,
				Target:     m.Destination,
				ReadOnly:   !m.RW,
			})
		}
	}

	networkName := fmt.Sprintf("net_user_%s", ownerID.String())
	fullDomain := fmt.Sprintf("%s.%s", domainPrefix, s.config.BaseDomain)

	dockerParams := docker.CreateContainerParams{
		ContainerName:     strings.TrimPrefix(inspect.Name, "/"),
		ImageName:         inspect.Config.Image,
		NetworkName:       networkName,
		Domain:            fullDomain,
		InternalPort:      internalPort,
		EnvVars:           envList,
		MemoryLimitBytes:  inspect.HostConfig.Memory,
		MemoryReservation: inspect.HostConfig.MemoryReservation,
		CPUShares:         inspect.HostConfig.CPUShares,
		VolumeMounts:      dockerMounts,
		MaxLogSize:        s.config.MaxLogSize,
		MaxLogFiles:       s.config.MaxLogFiles,
		StorageQuota:      s.config.ContainerDiskQuota,
	}

	// Создаем новый контейнер с лейблами Traefik
	newDockerID, err := s.dockerAPI.CreateContainer(ctx, dockerParams)
	if err != nil {
		return err
	}

	// Обновляем DockerID, домен и порт в базе данных
	err = s.repo.UpdateDockerID(ctx, containerID, newDockerID)
	if err != nil {
		return err
	}
	err = s.repo.UpdateRouting(ctx, containerID, domainPrefix, internalPort)
	if err != nil {
		return err
	}

	// Если до пересоздания контейнер был запущен — запускаем новый
	if c.Status == domain.ContainerStatusRunning {
		err = s.dockerAPI.StartContainer(ctx, newDockerID)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *ContainerService) Start(ctx context.Context, ownerID, containerID uuid.UUID) error {
	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return err
	}
	if c.OwnerID != ownerID {
		return apperrors.ErrNotFound
	}

	// Повторная проверка перед стартом (вдруг пока он был 'exited', студент запустил другие)
	if err := s.checkUserQuota(ctx, ownerID, c.BaseMemoryReservation); err != nil {
		return err
	}
	if err := s.checkHostCapacity(c.BaseMemoryReservation); err != nil {
		return err
	}

	if err := s.dockerAPI.StartContainer(ctx, c.DockerID); err != nil {
		return err
	}

	err = s.repo.UpdateStatus(ctx, containerID, domain.ContainerStatusRunning)

	// Вызываем ребалансировку в фоне
	if err == nil {
		go s.RebalanceResources(context.Background())
	}

	return err
}

func (s *ContainerService) Stop(ctx context.Context, ownerID, containerID uuid.UUID) error {
	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return err
	}
	if c.OwnerID != ownerID {
		return apperrors.ErrNotFound
	}

	if err := s.dockerAPI.StopContainer(ctx, c.DockerID, s.config.ContainerStopTimeout); err != nil {
		return err
	}

	err = s.repo.UpdateStatus(ctx, containerID, domain.ContainerStatusExited)

	// Кто-то остановился -> освободились ресурсы -> ребалансируем остальных!
	if err == nil {
		go s.RebalanceResources(context.Background())
	}

	return err
}

func (s *ContainerService) Delete(ctx context.Context, ownerID, containerID uuid.UUID) error {
	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return err
	}
	if c.OwnerID != ownerID {
		return apperrors.ErrNotFound
	}

	err = s.dockerAPI.RemoveContainer(ctx, c.DockerID, true)
	if err != nil {
		return err
	}

	return s.repo.Delete(ctx, containerID)
}

func (s *ContainerService) GetByOwner(ctx context.Context, ownerID uuid.UUID) ([]domain.Container, error) {
	return s.repo.GetByOwnerID(ctx, ownerID)
}

// --- Admission Control ---

func (s *ContainerService) checkUserQuota(ctx context.Context, ownerID uuid.UUID, requestedRam int64) error {
	userQuota, err := s.repo.GetUserRAMQuota(ctx, ownerID)
	if err != nil {
		return err
	}

	usedRam, err := s.repo.GetUserReservedMemory(ctx, ownerID)
	if err != nil {
		return err
	}

	if usedRam+requestedRam > userQuota {
		return apperrors.ErrQuotaExceeded
	}
	return nil
}

func (s *ContainerService) checkHostCapacity(requestedRam int64) error {
	totalMem, err := s.metrics.GetTotalMemory()
	if err != nil {
		log.Printf("[AdmissionControl] Failed to get system memory: %v", err)
		return apperrors.ErrInternal
	}

	// 1. Calculate the maximum allowed memory pool (considering overcommit)
	// Example: Total Mem 16GB, Reserved 2GB -> 14GB available.
	// Overcommit 1.5 -> Max Pool = 21GB.
	availablePool := float64(totalMem-s.config.ReservedSystemMemory) * s.config.OvercommitFactor

	// If the system is so constrained that the pool is zero or negative
	if availablePool <= 0 {
		return apperrors.ErrHostExhausted
	}

	// 2. Calculate the currently reserved RAM by ALL running containers across the entire system.
	// We need a repository method to get the total reserved RAM for ALL users, not just one.
	totalRunningReserved, err := s.repo.GetTotalSystemReservedMemory(context.Background())
	if err != nil {
		log.Printf("[AdmissionControl] Failed to calculate total system reserved memory: %v", err)
		return apperrors.ErrInternal
	}

	// 3. Admission Check: Will adding this new container push us over the overcommit limit?
	projectedRequiredMem := totalRunningReserved + requestedRam
	if projectedRequiredMem > int64(availablePool) {
		log.Printf("[AdmissionControl] Request rejected. Projected: %d MB, Max Pool: %d MB", projectedRequiredMem/1024/1024, int64(availablePool)/1024/1024)
		return apperrors.ErrHostExhausted
	}

	return nil
}

// --- Dynamic Rebalancing (The "Robin Hood" algorithm) ---

func (s *ContainerService) RebalanceResources(ctx context.Context) {
	runningContainers, err := s.repo.GetRunning(ctx)
	if err != nil || len(runningContainers) == 0 {
		return
	}

	totalMem, err := s.metrics.GetTotalMemory()
	if err != nil {
		log.Printf("[Rebalancer] Error reading memory: %v", err)
		return
	}

	// Свободная память на сервере для "Burst" режима
	availableForBurst := totalMem - s.config.ReservedSystemMemory

	// Сколько памяти гарантированно забрали все текущие запущенные контейнеры
	var totalReserved int64 = 0
	for _, c := range runningContainers {
		totalReserved += c.BaseMemoryReservation
	}

	// Рассчитываем множитель (Burst Factor)
	// Например: Сервер 10ГБ. Зарезервировано 2ГБ. Фактор = 5.0
	// Значит каждый контейнер может получить жесткий лимит в 5 раз больше его гарантии.
	var burstFactor float64 = 1.0
	if totalReserved > 0 && availableForBurst > totalReserved {
		burstFactor = float64(availableForBurst) / float64(totalReserved)
	}

	// Применяем новые жесткие лимиты (Memory) к Docker Engine
	for _, c := range runningContainers {
		newMemoryLimit := int64(float64(c.BaseMemoryReservation) * burstFactor)

		// Ограничиваем сверху, чтобы один контейнер не съел весь хост (например, не больше 4x от базы)
		maxAllowedBurst := c.BaseMemoryReservation * s.config.MaxBurstMultiplier
		if newMemoryLimit > maxAllowedBurst {
			newMemoryLimit = maxAllowedBurst
		}

		// Выдаем CpuShares: если мало контейнеров - высокий приоритет, если много - стандартный
		cpuShares := s.config.DefaultCPUShares
		if len(runningContainers) > s.config.HighLoadContainerCount {
			cpuShares = s.config.HighLoadCPUShares
		}

		err := s.dockerAPI.UpdateContainerResources(ctx, c.DockerID, newMemoryLimit, c.BaseMemoryReservation, cpuShares)
		if err != nil {
			log.Printf("[Rebalancer] failed to update %s: %v", c.ID, err)
		}
	}
	log.Printf("[Rebalancer] Rebalanced %d containers. Burst Factor: %.2f", len(runningContainers), burstFactor)
}
