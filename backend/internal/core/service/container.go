package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/google/uuid"
)

type ContainerRepository interface {
	Save(ctx context.Context, c model.Container) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Container, error)
	GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]model.Container, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	UpdateDockerID(ctx context.Context, id uuid.UUID, dockerID string) error
	UpdateDockerIDAndStatus(ctx context.Context, id uuid.UUID, dockerID string, status string) error
	UpdateRouting(ctx context.Context, id uuid.UUID, domainPrefix string, internalPort int) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetUserReservedMemory(ctx context.Context, ownerID uuid.UUID) (int64, error)
	GetRunning(ctx context.Context) ([]model.Container, error)
	CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error)
	GetTotalSystemReservedMemory(ctx context.Context) (int64, error)
	GetNonExited(ctx context.Context) ([]model.Container, error)
	CheckDomainPrefixExists(ctx context.Context, prefix string) (bool, error)
	GetAllPaginated(ctx context.Context, limit, offset int) ([]model.Container, int, error)
}

type ContainerVolumeRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (model.Volume, error)
	SaveMounts(ctx context.Context, mounts []model.VolumeMount) error
}

type ContainerDockerAPI interface {
	EnsureUserNetwork(ctx context.Context, networkName string) (string, error)
	RemoveNetwork(ctx context.Context, networkName string) error
	PullImage(ctx context.Context, imageName string) error
	CreateContainer(ctx context.Context, params docker.CreateContainerParams) (string, error)
	StartContainer(ctx context.Context, dockerID string) error
	StopContainer(ctx context.Context, dockerID string, timeout int) error
	RemoveContainer(ctx context.Context, dockerID string, force bool) error
	UpdateContainerResources(ctx context.Context, dockerID string, memoryLimit, memoryReservation, cpuShares int64) error
	InspectContainer(ctx context.Context, dockerID string) (*container.InspectResponse, error)
	ImageExists(ctx context.Context, imageTag string) (bool, error)
	GetContainerStats(ctx context.Context, dockerID string) (model.ContainerStats, error)
}

type HostMetricsProvider interface {
	GetTotalMemory() (int64, error)
	GetFreeMemory() (int64, error)
}

type ContainerImageRepository interface {
	GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]model.Image, error)
}

type ConfigManager interface {
	Get() config.SystemConfig
}

type UserInfoProvider interface {
	GetUser(ctx context.Context, userID uuid.UUID) (model.UserInfo, error)
}

type ContainerService struct {
	repo       ContainerRepository
	volumeRepo ContainerVolumeRepository
	imageRepo  ContainerImageRepository
	dockerAPI  ContainerDockerAPI
	metrics    HostMetricsProvider
	config     ConfigManager
	users      UserInfoProvider
	logger     *slog.Logger
}

func NewContainerService(
	repo ContainerRepository,
	volumeRepo ContainerVolumeRepository,
	imageRepo ContainerImageRepository,
	dockerAPI ContainerDockerAPI,
	metrics HostMetricsProvider,
	config ConfigManager,
	users UserInfoProvider,
	logger *slog.Logger,
) *ContainerService {
	return &ContainerService{
		repo:       repo,
		volumeRepo: volumeRepo,
		imageRepo:  imageRepo,
		dockerAPI:  dockerAPI,
		metrics:    metrics,
		config:     config,
		users:      users,
		logger:     logging.WithComponent(logger, "container_service"),
	}
}

func (s *ContainerService) Create(ctx context.Context, ownerID uuid.UUID, params model.ContainerCreateParams) (uuid.UUID, error) {
	if err := validation.ResourceName(params.Name); err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	if err := validation.ImageTag(params.ImageTag); err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	if params.InternalPort < 0 || params.InternalPort > 65535 {
		return uuid.Nil, fmt.Errorf("%w: invalid internal port", apperrors.ErrBadRequest)
	}
	if err := validation.DomainPrefix(params.DomainPrefix); err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}

	count, err := s.repo.CountByOwnerID(ctx, ownerID)
	if err != nil {
		return uuid.Nil, err
	}
	if count >= s.config.Get().MaxContainersPerUser {
		return uuid.Nil, apperrors.ErrLimitExceeded
	}

	var fullDomain string
	if params.DomainPrefix != "" {
		if params.InternalPort <= 0 {
			return uuid.Nil, apperrors.ErrBadRequest
		}

		exists, err := s.repo.CheckDomainPrefixExists(ctx, params.DomainPrefix)
		if err != nil {
			return uuid.Nil, err
		}
		if exists {
			return uuid.Nil, fmt.Errorf("%w: domain prefix already in use", apperrors.ErrAlreadyExists)
		}

		fullDomain = fmt.Sprintf("%s.%s", params.DomainPrefix, s.config.Get().BaseDomain)
	}

	// 1. Определение запрашиваемой памяти (Гарантии)
	reqMem := params.RequestedMemoryMB * 1024 * 1024
	if reqMem <= 0 {
		reqMem = s.config.Get().DefaultMemoryReservation
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

	baseName, version := parseImageTag(params.ImageTag)
	normalizedInputTag := fmt.Sprintf("%s:%s", baseName, version)

	isCustom := false
	userImages, err := s.imageRepo.GetByOwnerID(ctx, ownerID)
	if err == nil {
		for _, img := range userImages {
			// Сравниваем с нормализованным тегом из БД
			if img.Tag == normalizedInputTag && img.IsCustom {
				isCustom = true
				break
			}
		}
	}

	actualImageTag := normalizedInputTag
	if isCustom {
		// Формируем полный тег для пулла из Registry
		repoName := strings.ToLower(fmt.Sprintf("%s_%s", ownerID.String(), baseName))
		actualImageTag = fmt.Sprintf("%s/%s:%s", s.config.Get().RegistryPublicURL, repoName, version)
	}

	// Если образ кастомный — ПУЛЛИМ ВСЕГДА (вдруг пользователь пересобрал его)
	// Если публичный — пуллим только если его нет на хосте
	if isCustom {
		err = s.dockerAPI.PullImage(ctx, actualImageTag)
		if err != nil {
			return uuid.Nil, apperrors.Wrap(apperrors.ErrBadRequest, "image could not be pulled", err)
		}
	} else {
		imageExists, err := s.dockerAPI.ImageExists(ctx, actualImageTag)
		if err != nil {
			return uuid.Nil, err
		}
		if !imageExists {
			err = s.dockerAPI.PullImage(ctx, actualImageTag)
			if err != nil {
				return uuid.Nil, apperrors.Wrap(apperrors.ErrBadRequest, "image could not be pulled", err)
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
	var dbMounts []model.VolumeMount

	for _, vm := range params.VolumeMounts {
		if err := validation.MountPath(vm.MountPath); err != nil {
			return uuid.Nil, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
		}

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

		dbMounts = append(dbMounts, model.VolumeMount{
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

	var ttlDeadline *time.Time
	if s.config.Get().ContainerTTL > 0 {
		t := time.Now().Add(s.config.Get().ContainerTTL)
		ttlDeadline = &t
	}

	c := model.Container{
		ID:                    containerID,
		ProjectID:             params.ProjectID,
		OwnerID:               ownerID,
		Name:                  params.Name,
		ImageTag:              normalizedInputTag,
		InternalPort:          params.InternalPort,
		DomainPrefix:          params.DomainPrefix,
		Status:                model.ContainerStatusCreating,
		TTLDeadline:           ttlDeadline,
		EnvVars:               envBytes,
		BaseMemoryReservation: reqMem,
	}

	if err := s.repo.Save(ctx, c); err != nil {
		return uuid.Nil, err
	}

	// 5. Конфигурация Docker. Изначально ставим жесткий лимит равным мягкому.
	// Ребалансировщик потом его увеличит (Burst).
	dockerParams := docker.CreateContainerParams{
		ContainerName:     fmt.Sprintf("usr_%s", containerID.String()[:12]),
		NetworkAlias:      params.NetworkAlias,
		ImageName:         actualImageTag,
		NetworkName:       networkName,
		Domain:            fullDomain,
		InternalPort:      params.InternalPort,
		EnvVars:           envList,
		MemoryLimitBytes:  reqMem,                          // Стартовый жесткий лимит
		MemoryReservation: reqMem,                          // Гарантия (Soft limit)
		CPUShares:         s.config.Get().DefaultCPUShares, // Базовый приоритет
		VolumeMounts:      dockerMounts,
		MaxLogSize:        s.config.Get().MaxLogSize,
		MaxLogFiles:       s.config.Get().MaxLogFiles,
		StorageQuota:      s.config.Get().ContainerDiskQuota,
		Command:           params.Command,
		Entrypoint:        params.Entrypoint,
		Restart:           params.Restart,
		Healthcheck:       params.Healthcheck,
	}

	dockerID, err := s.dockerAPI.CreateContainer(ctx, dockerParams)
	if err != nil {
		_ = s.repo.Delete(ctx, containerID)
		return uuid.Nil, err
	}

	if len(dbMounts) > 0 {
		if err := s.volumeRepo.SaveMounts(ctx, dbMounts); err != nil {
			s.dockerAPI.RemoveContainer(context.Background(), dockerID, true)
			_ = s.repo.Delete(ctx, containerID)
			return uuid.Nil, err
		}
	}

	err = s.repo.UpdateDockerIDAndStatus(ctx, containerID, dockerID, model.ContainerStatusCreated)
	if err != nil {
		s.dockerAPI.RemoveContainer(context.Background(), dockerID, true)
		_ = s.repo.Delete(ctx, containerID)
		return uuid.Nil, err
	}

	s.logger.InfoContext(ctx, "container created", "container_id", containerID, "owner_id", ownerID, "image_tag", normalizedInputTag)
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

	if internalPort > 65535 {
		return fmt.Errorf("%w: invalid internal port", apperrors.ErrBadRequest)
	}

	if err := validation.DomainPrefix(domainPrefix); err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}

	if c.DomainPrefix != domainPrefix {
		exists, err := s.repo.CheckDomainPrefixExists(ctx, domainPrefix)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("%w: domain prefix already in use", apperrors.ErrAlreadyExists)
		}
	}

	inspect, err := s.dockerAPI.InspectContainer(ctx, c.DockerID)
	if err != nil {
		return err
	}

	err = s.dockerAPI.StopContainer(ctx, c.DockerID, s.config.Get().ContainerStopTimeout)
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

	var healthcheck *model.Healthcheck
	if inspect.Config.Healthcheck != nil {
		healthcheck = &model.Healthcheck{
			Test:        inspect.Config.Healthcheck.Test,
			Interval:    inspect.Config.Healthcheck.Interval,
			Timeout:     inspect.Config.Healthcheck.Timeout,
			StartPeriod: inspect.Config.Healthcheck.StartPeriod,
			Retries:     inspect.Config.Healthcheck.Retries,
		}
	}

	networkName := fmt.Sprintf("net_user_%s", ownerID.String())
	fullDomain := fmt.Sprintf("%s.%s", domainPrefix, s.config.Get().BaseDomain)

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
		MaxLogSize:        s.config.Get().MaxLogSize,
		MaxLogFiles:       s.config.Get().MaxLogFiles,
		StorageQuota:      s.config.Get().ContainerDiskQuota,
		Command:           inspect.Config.Cmd,
		Entrypoint:        inspect.Config.Entrypoint,
		Restart:           string(inspect.HostConfig.RestartPolicy.Name),
		Healthcheck:       healthcheck,
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
	if c.Status == model.ContainerStatusRunning {
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

	err = s.repo.UpdateStatus(ctx, containerID, model.ContainerStatusRunning)

	// Вызываем ребалансировку в фоне
	if err == nil {
		s.logger.InfoContext(ctx, "container started", "container_id", containerID, "owner_id", ownerID)
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

	if err := s.dockerAPI.StopContainer(ctx, c.DockerID, s.config.Get().ContainerStopTimeout); err != nil {
		return err
	}

	err = s.repo.UpdateStatus(ctx, containerID, model.ContainerStatusExited)

	// Кто-то остановился -> освободились ресурсы -> ребалансируем остальных!
	if err == nil {
		s.logger.InfoContext(ctx, "container stopped", "container_id", containerID, "owner_id", ownerID)
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

	err = s.repo.Delete(ctx, containerID)
	if err != nil {
		return err
	}

	count, _ := s.repo.CountByOwnerID(ctx, ownerID)
	if count == 0 {
		networkName := fmt.Sprintf("net_user_%s", ownerID.String())
		_ = s.dockerAPI.RemoveNetwork(ctx, networkName)
	}

	s.logger.InfoContext(ctx, "container deleted", "container_id", containerID, "owner_id", ownerID)
	return nil
}

func (s *ContainerService) GetByOwner(ctx context.Context, ownerID uuid.UUID) ([]model.Container, error) {
	return s.repo.GetByOwnerID(ctx, ownerID)
}

func (s *ContainerService) GetByID(ctx context.Context, id uuid.UUID) (model.Container, error) {
	return s.repo.GetByID(ctx, id)
}

// --- Admission Control ---

func (s *ContainerService) checkUserQuota(ctx context.Context, ownerID uuid.UUID, requestedRam int64) error {
	user, err := s.users.GetUser(ctx, ownerID)
	if err != nil {
		return err
	}
	userQuota := user.QuotaRAMMB * 1024 * 1024

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
		s.logger.Error("failed to get system memory", "error", err)
		return apperrors.ErrInternal
	}

	// 1. Calculate the maximum allowed memory pool (considering overcommit)
	// Example: Total Mem 16GB, Reserved 2GB -> 14GB available.
	// Overcommit 1.5 -> Max Pool = 21GB.
	availablePool := float64(totalMem-s.config.Get().ReservedSystemMemory) * s.config.Get().OvercommitFactor

	// If the system is so constrained that the pool is zero or negative
	if availablePool <= 0 {
		return apperrors.ErrHostExhausted
	}

	// 2. Calculate the currently reserved RAM by ALL running containers across the entire system.
	// We need a repository method to get the total reserved RAM for ALL users, not just one.
	totalRunningReserved, err := s.repo.GetTotalSystemReservedMemory(context.Background())
	if err != nil {
		s.logger.Error("failed to calculate total system reserved memory", "error", err)
		return apperrors.ErrInternal
	}

	// 3. Admission Check: Will adding this new container push us over the overcommit limit?
	projectedRequiredMem := totalRunningReserved + requestedRam
	if projectedRequiredMem > int64(availablePool) {
		s.logger.Warn(
			"container request rejected by host capacity",
			"projected_memory_mb", projectedRequiredMem/1024/1024,
			"max_pool_mb", int64(availablePool)/1024/1024,
		)
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
		s.logger.ErrorContext(ctx, "failed to read system memory for rebalancing", "error", err)
		return
	}

	// Свободная память на сервере для "Burst" режима
	availableForBurst := totalMem - s.config.Get().ReservedSystemMemory

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
		maxAllowedBurst := c.BaseMemoryReservation * s.config.Get().MaxBurstMultiplier
		if newMemoryLimit > maxAllowedBurst {
			newMemoryLimit = maxAllowedBurst
		}

		// Выдаем CpuShares: если мало контейнеров - высокий приоритет, если много - стандартный
		cpuShares := s.config.Get().DefaultCPUShares
		if len(runningContainers) > s.config.Get().HighLoadContainerCount {
			cpuShares = s.config.Get().HighLoadCPUShares
		}

		err := s.dockerAPI.UpdateContainerResources(ctx, c.DockerID, newMemoryLimit, c.BaseMemoryReservation, cpuShares)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to update container resources", "container_id", c.ID, "docker_id", c.DockerID, "error", err)
		}
	}
	s.logger.InfoContext(ctx, "containers rebalanced", "container_count", len(runningContainers), "burst_factor", burstFactor)
}

func (s *ContainerService) GetAllPaginated(ctx context.Context, limit, offset int) ([]model.Container, int, error) {
	return s.repo.GetAllPaginated(ctx, limit, offset)
}

func (s *ContainerService) AdminDelete(ctx context.Context, containerID uuid.UUID) error {
	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return err
	}
	_ = s.dockerAPI.RemoveContainer(ctx, c.DockerID, true)
	return s.repo.Delete(ctx, containerID)
}

func (s *ContainerService) AdminStart(ctx context.Context, containerID uuid.UUID) error {
	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return err
	}
	if err := s.dockerAPI.StartContainer(ctx, c.DockerID); err != nil {
		return err
	}
	err = s.repo.UpdateStatus(ctx, containerID, model.ContainerStatusRunning)
	if err == nil {
		s.logger.InfoContext(ctx, "container started by admin", "container_id", containerID)
		go s.RebalanceResources(context.Background())
	}
	return err
}

func (s *ContainerService) AdminStop(ctx context.Context, containerID uuid.UUID) error {
	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return err
	}
	if err := s.dockerAPI.StopContainer(ctx, c.DockerID, s.config.Get().ContainerStopTimeout); err != nil {
		return err
	}
	err = s.repo.UpdateStatus(ctx, containerID, model.ContainerStatusExited)
	if err == nil {
		s.logger.InfoContext(ctx, "container stopped by admin", "container_id", containerID)
		go s.RebalanceResources(context.Background())
	}
	return err
}

func (s *ContainerService) GetStats(ctx context.Context, ownerID, containerID uuid.UUID) (model.ContainerStats, error) {
	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return model.ContainerStats{}, err
	}
	if c.OwnerID != ownerID {
		return model.ContainerStats{}, apperrors.ErrNotFound
	}
	if c.Status != model.ContainerStatusRunning {
		return model.ContainerStats{}, nil
	}
	return s.dockerAPI.GetContainerStats(ctx, c.DockerID)
}

func (s *ContainerService) AdminGetStats(ctx context.Context, containerID uuid.UUID) (model.ContainerStats, error) {
	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return model.ContainerStats{}, err
	}
	if c.Status != model.ContainerStatusRunning {
		return model.ContainerStats{}, nil
	}
	return s.dockerAPI.GetContainerStats(ctx, c.DockerID)
}
