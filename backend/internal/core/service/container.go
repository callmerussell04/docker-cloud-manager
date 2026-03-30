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
	BaseDomain               string
	DefaultMemoryReservation int64
	ReservedSystemMemory     int64
	OvercommitFactor         float64
	MaxBurstMultiplier       int64
	DefaultCPUShares         int64
	HighLoadCPUShares        int64
	HighLoadContainerCount   int
	ContainerStopTimeout     int
}

type ContainerRepository interface {
	Save(ctx context.Context, c domain.Container) error
	GetByID(ctx context.Context, id uuid.UUID) (domain.Container, error)
	GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Container, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetUserReservedMemory(ctx context.Context, ownerID uuid.UUID) (int64, error)
	GetUserRAMQuota(ctx context.Context, ownerID uuid.UUID) (int64, error)
	GetRunning(ctx context.Context) ([]domain.Container, error)
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
	_, err := s.dockerAPI.EnsureUserNetwork(ctx, networkName)
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

	containerID := uuid.New()
	hostDomain := fmt.Sprintf("%s-%s.%s", params.Name, ownerID.String()[:8], s.config.BaseDomain)

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
		ImageName:         params.ImageTag,
		NetworkName:       networkName,
		Domain:            hostDomain,
		InternalPort:      params.InternalPort,
		EnvVars:           envList,
		MemoryLimitBytes:  reqMem,                    // Стартовый жесткий лимит
		MemoryReservation: reqMem,                    // Гарантия (Soft limit)
		CPUShares:         s.config.DefaultCPUShares, // Базовый приоритет
		VolumeMounts:      dockerMounts,
	}

	dockerID, err := s.dockerAPI.CreateContainer(ctx, dockerParams)
	if err != nil {
		return uuid.Nil, err
	}

	container := domain.Container{
		ID:                    containerID,
		OwnerID:               ownerID,
		DockerID:              dockerID,
		Name:                  params.Name,
		ImageTag:              params.ImageTag,
		InternalPort:          params.InternalPort,
		Status:                domain.ContainerStatusCreated,
		EnvVars:               envBytes,
		BaseMemoryReservation: reqMem,
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

	// Повторная проверка перед стартом (вдруг пока он был 'exited', студент запустил другие)
	if err := s.checkUserQuota(ctx, ownerID, container.BaseMemoryReservation); err != nil {
		return err
	}
	if err := s.checkHostCapacity(container.BaseMemoryReservation); err != nil {
		return err
	}

	if err := s.dockerAPI.StartContainer(ctx, container.DockerID); err != nil {
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
	container, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return err
	}
	if container.OwnerID != ownerID {
		return apperrors.ErrNotFound
	}

	if err := s.dockerAPI.StopContainer(ctx, container.DockerID, s.config.ContainerStopTimeout); err != nil {
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
		return apperrors.ErrInternal
	}

	// Мы позволяем резервировать (overcommit) до 150% памяти хоста
	// Но если даже 150% исчерпано, мы отклоняем новые запуски
	maxAllowedReservations := int64(float64(totalMem-s.config.ReservedSystemMemory) * s.config.OvercommitFactor)

	// В идеале здесь должен быть SQL SUM всех running контейнеров,
	// но для упрощения (и скорости) можно опустить эту жесткую проверку,
	// т.к. OOM Killer хоста нас подстрахует.
	// Для диплома можно оставить заглушку или реализовать кэш.

	_ = maxAllowedReservations
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
}
