package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
)

type ContainerRepository interface {
	Save(ctx context.Context, c model.Container) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Container, error)
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
	List(ctx context.Context, opts model.ListOptions) ([]model.Container, int, error)
}

type containerCreateRepository interface {
	SaveWithMountsAndOperation(ctx context.Context, c model.Container, mounts []model.VolumeMount, op model.ResourceOperation, lockOwner, lockCapacity bool) error
}

type containerStateRepository interface {
	SetDesiredStatus(ctx context.Context, id uuid.UUID, desiredStatus string) error
	MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error
	CreateOperation(ctx context.Context, op model.ResourceOperation) error
	CompleteLatestOperation(ctx context.Context, resourceType string, resourceID uuid.UUID, status string, cause error) error
}

type containerLockRepository interface {
	AcquireOwnerCapacityLock(ctx context.Context, ownerID uuid.UUID) (func(), error)
}

type containerExposeRepository interface {
	UpdateDockerIDRoutingAndGeneration(ctx context.Context, id uuid.UUID, dockerID string, domainPrefix string, internalPort int, generation int) error
}

type ContainerVolumeRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (model.Volume, error)
	SaveMounts(ctx context.Context, mounts []model.VolumeMount) error
}

type ContainerDockerAPI interface {
	EnsureUserNetwork(ctx context.Context, networkName string) (string, error)
	RemoveNetwork(ctx context.Context, networkName string) error
	PullImage(ctx context.Context, imageName string) error
	CreateContainer(ctx context.Context, params model.ContainerRuntimeSpec) (string, error)
	StartContainer(ctx context.Context, dockerID string) error
	StopContainer(ctx context.Context, dockerID string, timeout int) error
	RemoveContainer(ctx context.Context, dockerID string, force bool) error
	UpdateContainerResources(ctx context.Context, dockerID string, memoryLimit, memoryReservation, cpuShares int64, memorySwapMultiplier float64) error
	InspectContainer(ctx context.Context, dockerID string) (model.ContainerInspection, error)
	ImageExists(ctx context.Context, imageTag string) (bool, error)
	GetContainerStats(ctx context.Context, dockerID string) (model.ContainerStats, error)
}

type HostMetricsProvider interface {
	GetTotalMemory() (int64, error)
	GetFreeMemory() (int64, error)
}

type ContainerImageRepository interface {
	List(ctx context.Context, opts model.ListOptions) ([]model.Image, int, error)
}

type containerImageDiskRepository interface {
	GetUserUsedDiskSpace(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

type containerVolumeInspector interface {
	InspectVolume(ctx context.Context, volumeName string) (model.VolumeInspection, error)
}

type ConfigManager interface {
	Get() config.SystemConfig
}

type UserInfoProvider interface {
	GetUser(ctx context.Context, userID uuid.UUID) (model.UserInfo, error)
}

type ProjectStatusUpdater interface {
	RefreshProjectStatus(ctx context.Context, projectID uuid.UUID) error
}

type ContainerService struct {
	repo        ContainerRepository
	volumeRepo  ContainerVolumeRepository
	imageRepo   ContainerImageRepository
	dockerAPI   ContainerDockerAPI
	metrics     HostMetricsProvider
	config      ConfigManager
	users       UserInfoProvider
	projects    ProjectStatusUpdater
	logger      *slog.Logger
	rebalanceCh chan struct{}
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
		repo:        repo,
		volumeRepo:  volumeRepo,
		imageRepo:   imageRepo,
		dockerAPI:   dockerAPI,
		metrics:     metrics,
		config:      config,
		users:       users,
		logger:      logging.WithComponent(logger, "container_service"),
		rebalanceCh: make(chan struct{}, 1),
	}
}

func (s *ContainerService) SetProjectStatusUpdater(updater ProjectStatusUpdater) {
	s.projects = updater
}

func (s *ContainerService) Create(ctx context.Context, params model.ContainerCreateParams) (createdID uuid.UUID, err error) {
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		return uuid.Nil, err
	}
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
	if validation.ReservedDomainPrefix(params.DomainPrefix, s.config.Get().ReservedDomainPrefixes) {
		return uuid.Nil, fmt.Errorf("%w: domain prefix is reserved", apperrors.ErrBadRequest)
	}

	unlock, err := s.acquireOwnerCapacityLock(ctx, ownerID)
	if err != nil {
		return uuid.Nil, err
	}
	if unlock != nil {
		defer unlock()
	}

	count, err := s.repo.CountByOwnerID(ctx, ownerID)
	if err != nil {
		return uuid.Nil, err
	}
	cfg := s.config.Get()
	if count >= cfg.MaxContainersPerUser {
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

		fullDomain = fmt.Sprintf("%s.%s", params.DomainPrefix, cfg.BaseDomain)
	}

	// 1. Определение запрашиваемой памяти (Гарантии)
	reqMem := params.RequestedMemoryMB * 1024 * 1024
	if reqMem <= 0 {
		reqMem = cfg.DefaultMemoryReservation
	}

	// 2. Admission Control: Проверка квоты пользователя
	if err := s.checkUserQuota(ctx, ownerID, reqMem); err != nil {
		return uuid.Nil, err
	}
	if err := s.checkUserDiskQuota(ctx, ownerID); err != nil {
		return uuid.Nil, err
	}

	// 3. Admission Control: Проверка свободных ресурсов хоста (Защита сервера)
	if err := s.checkHostCapacity(ctx, reqMem); err != nil {
		return uuid.Nil, err
	}

	// 4. Изоляция сети
	networkName := userNetworkName(ownerID)
	_, err = s.dockerAPI.EnsureUserNetwork(ctx, networkName)
	if err != nil {
		return uuid.Nil, err
	}
	defer func() {
		if err != nil {
			s.cleanupUserNetworkIfUnused(ctx, ownerID)
		}
	}()

	baseName, version := parseImageTag(params.ImageTag)
	normalizedInputTag := fmt.Sprintf("%s:%s", baseName, version)

	isCustom := false
	var customImage *model.Image
	userImages, _, err := s.imageRepo.List(ctx, model.ListOptions{OwnerID: &ownerID})
	if err == nil {
		for _, img := range userImages {
			// Сравниваем с нормализованным тегом из БД
			if img.Tag == normalizedInputTag {
				isCustom = true
				customImage = &img
				break
			}
		}
	}
	if customImage != nil && customImage.Status == model.ImageStatusMissing {
		return uuid.Nil, resourceUnavailableError("image")
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
			if customImage != nil && isDockerNotFound(err) {
				s.markImageMissing(ctx, customImage.ID)
			}
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
	var dockerMounts []model.ContainerMountSpec
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
		if vol.Status == model.VolumeStatusMissing {
			return uuid.Nil, resourceUnavailableError("volume")
		}
		if inspector, ok := s.dockerAPI.(containerVolumeInspector); ok {
			if _, err := inspector.InspectVolume(ctx, vol.DockerName); err != nil {
				if isDockerNotFound(err) {
					s.markVolumeMissing(ctx, vol.ID)
					return uuid.Nil, resourceUnavailableError("volume")
				}
				return uuid.Nil, err
			}
		}

		dockerMounts = append(dockerMounts, model.ContainerMountSpec{
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
	if cfg.ContainerTTLHours > 0 {
		t := time.Now().Add(time.Duration(cfg.ContainerTTLHours) * time.Hour)
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
		DesiredStatus:         model.ContainerStatusCreated,
		TTLDeadline:           ttlDeadline,
		EnvVars:               envBytes,
		BaseMemoryReservation: reqMem,
		DockerGeneration:      1,
		NetworkAlias:          params.NetworkAlias,
		Command:               params.Command,
		Entrypoint:            params.Entrypoint,
		Restart:               params.Restart,
		Healthcheck:           params.Healthcheck,
	}

	op := model.ResourceOperation{
		ID:           uuid.New(),
		ResourceType: model.ResourceTypeContainer,
		ResourceID:   containerID,
		OwnerID:      ownerID,
		Operation:    model.OperationCreate,
		Status:       model.OperationStatusRunning,
	}
	if txRepo, ok := s.repo.(containerCreateRepository); ok {
		if err := txRepo.SaveWithMountsAndOperation(ctx, c, dbMounts, op, false, false); err != nil {
			return uuid.Nil, err
		}
	} else {
		if err := s.repo.Save(ctx, c); err != nil {
			return uuid.Nil, err
		}
		if len(dbMounts) > 0 {
			if err := s.volumeRepo.SaveMounts(ctx, dbMounts); err != nil {
				_ = s.repo.Delete(ctx, containerID)
				return uuid.Nil, err
			}
		}
	}

	// 5. Конфигурация Docker. Изначально ставим жесткий лимит равным мягкому.
	// Ребалансировщик потом его увеличит (Burst).
	dockerParams := model.ContainerRuntimeSpec{
		ContainerID:          containerID.String(),
		OwnerID:              ownerID.String(),
		Generation:           c.DockerGeneration,
		ContainerName:        fmt.Sprintf("usr_%s", containerID.String()[:12]),
		NetworkAlias:         params.NetworkAlias,
		ImageName:            actualImageTag,
		NetworkName:          networkName,
		Domain:               fullDomain,
		InternalPort:         params.InternalPort,
		EnvVars:              envList,
		MemoryLimitBytes:     reqMem, // Стартовый жесткий лимит
		MemoryReservation:    reqMem, // Гарантия (Soft limit)
		MemorySwapMultiplier: cfg.ContainerMemorySwapMultiplier,
		CPUShares:            cfg.DefaultCPUShares, // Базовый приоритет
		PidsLimit:            cfg.ContainerPidsLimit,
		ProxyNetworkName:     cfg.ProxyNetworkName,
		VolumeMounts:         dockerMounts,
		MaxLogSize:           cfg.MaxLogSize,
		MaxLogFiles:          cfg.MaxLogFiles,
		StorageQuota:         cfg.ContainerDiskQuota,
		Command:              params.Command,
		Entrypoint:           params.Entrypoint,
		Restart:              params.Restart,
		Healthcheck:          params.Healthcheck,
	}
	if params.ProjectID != nil {
		dockerParams.ProjectID = params.ProjectID.String()
	}

	dockerID, err := s.dockerAPI.CreateContainer(ctx, dockerParams)
	if err != nil {
		s.markContainerError(ctx, containerID, model.ContainerStatusError, err)
		s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
		return uuid.Nil, err
	}

	err = s.repo.UpdateDockerIDAndStatus(ctx, containerID, dockerID, model.ContainerStatusCreated)
	if err != nil {
		s.dockerAPI.RemoveContainer(context.Background(), dockerID, true)
		s.markContainerError(context.Background(), containerID, model.ContainerStatusError, err)
		s.completeContainerOperation(context.Background(), containerID, model.OperationStatusFailed, err)
		return uuid.Nil, err
	}

	s.completeContainerOperation(ctx, containerID, model.OperationStatusDone, nil)
	s.logger.InfoContext(ctx, "container created", "container_id", containerID, "owner_id", ownerID, "image_tag", normalizedInputTag)
	return containerID, nil
}

func (s *ContainerService) Expose(ctx context.Context, containerID uuid.UUID, domainPrefix string, internalPort int) error {
	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return err
	}
	if err := accessscope.RequireOwnerAccess(ctx, c.OwnerID); err != nil {
		return err
	}
	ownerID := c.OwnerID
	if c.Status == model.ContainerStatusMissing {
		return resourceUnavailableError("container")
	}
	if c.DockerID == "" {
		s.markContainerError(ctx, containerID, model.ContainerStatusMissing, resourceMissingError("container"))
		return resourceUnavailableError("container")
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
	if validation.ReservedDomainPrefix(domainPrefix, s.config.Get().ReservedDomainPrefixes) {
		return fmt.Errorf("%w: domain prefix is reserved", apperrors.ErrBadRequest)
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
		if isDockerNotFound(err) {
			s.markContainerError(ctx, containerID, model.ContainerStatusMissing, resourceMissingError("container"))
			return resourceUnavailableError("container")
		}
		return err
	}

	var envList []string
	envList = append(envList, inspect.Env...)

	var dockerMounts []model.ContainerMountSpec
	for _, m := range inspect.Mounts {
		dockerMounts = append(dockerMounts, m)
	}

	networkName := userNetworkName(ownerID)
	cfg := s.config.Get()
	fullDomain := fmt.Sprintf("%s.%s", domainPrefix, cfg.BaseDomain)
	nextGeneration := c.DockerGeneration + 1
	if nextGeneration <= 1 {
		nextGeneration = 2
	}

	dockerParams := model.ContainerRuntimeSpec{
		ContainerID:          containerID.String(),
		OwnerID:              ownerID.String(),
		Generation:           nextGeneration,
		ContainerName:        fmt.Sprintf("%s_g%d", strings.TrimPrefix(inspect.Name, "/"), nextGeneration),
		ImageName:            inspect.Image,
		NetworkName:          networkName,
		Domain:               fullDomain,
		InternalPort:         internalPort,
		EnvVars:              envList,
		MemoryLimitBytes:     inspect.MemoryLimitBytes,
		MemoryReservation:    inspect.MemoryReservation,
		MemorySwapMultiplier: cfg.ContainerMemorySwapMultiplier,
		CPUShares:            inspect.CPUShares,
		PidsLimit:            cfg.ContainerPidsLimit,
		ProxyNetworkName:     cfg.ProxyNetworkName,
		VolumeMounts:         dockerMounts,
		MaxLogSize:           cfg.MaxLogSize,
		MaxLogFiles:          cfg.MaxLogFiles,
		StorageQuota:         cfg.ContainerDiskQuota,
		Command:              inspect.Command,
		Entrypoint:           inspect.Entrypoint,
		Restart:              inspect.Restart,
		Healthcheck:          inspect.Healthcheck,
	}
	if c.ProjectID != nil {
		dockerParams.ProjectID = c.ProjectID.String()
	}

	s.createContainerOperation(ctx, containerID, ownerID, model.OperationExpose)

	// Создаем новый контейнер с лейблами Traefik до удаления старого.
	newDockerID, err := s.dockerAPI.CreateContainer(ctx, dockerParams)
	if err != nil {
		s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
		return err
	}
	newStarted := false
	defer func() {
		if err != nil {
			_ = s.dockerAPI.RemoveContainer(context.Background(), newDockerID, true)
		}
	}()

	if c.Status == model.ContainerStatusRunning {
		if err = s.dockerAPI.StartContainer(ctx, newDockerID); err != nil {
			s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
			return err
		}
		newStarted = true
	}

	// Обновляем DockerID, домен и порт в базе данных
	if exposeRepo, ok := s.repo.(containerExposeRepository); ok {
		err = exposeRepo.UpdateDockerIDRoutingAndGeneration(ctx, containerID, newDockerID, domainPrefix, internalPort, nextGeneration)
	} else {
		err = s.repo.UpdateDockerID(ctx, containerID, newDockerID)
		if err == nil {
			err = s.repo.UpdateRouting(ctx, containerID, domainPrefix, internalPort)
		}
	}
	if err != nil {
		if newStarted {
			_ = s.dockerAPI.StopContainer(context.Background(), newDockerID, s.config.Get().ContainerStopTimeout)
		}
		s.completeContainerOperation(context.Background(), containerID, model.OperationStatusFailed, err)
		return err
	}

	if c.DockerID != "" {
		if removeErr := s.dockerAPI.RemoveContainer(ctx, c.DockerID, true); removeErr != nil && !isDockerNotFound(removeErr) {
			s.logger.WarnContext(ctx, "failed to remove old exposed container generation", "container_id", containerID, "docker_id", c.DockerID, "error", removeErr)
		}
	}

	s.completeContainerOperation(ctx, containerID, model.OperationStatusDone, nil)
	err = nil
	return nil
}

func (s *ContainerService) Start(ctx context.Context, containerID uuid.UUID) error {
	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return err
	}
	if err := accessscope.RequireOwnerAccess(ctx, c.OwnerID); err != nil {
		return err
	}
	ownerID := c.OwnerID
	if c.Status == model.ContainerStatusMissing {
		return resourceUnavailableError("container")
	}
	if c.DockerID == "" {
		s.markContainerError(ctx, containerID, model.ContainerStatusMissing, resourceMissingError("container"))
		return resourceUnavailableError("container")
	}

	unlock, err := s.acquireOwnerCapacityLock(ctx, ownerID)
	if err != nil {
		return err
	}
	if unlock != nil {
		defer unlock()
	}
	s.createContainerOperation(ctx, containerID, ownerID, model.OperationStart)

	// Повторная проверка перед стартом (вдруг пока он был 'exited', студент запустил другие)
	if err := s.checkUserQuota(ctx, ownerID, c.BaseMemoryReservation); err != nil {
		s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
		return err
	}
	if err := s.checkHostCapacity(ctx, c.BaseMemoryReservation); err != nil {
		s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
		return err
	}
	if err := s.checkUserDiskQuota(ctx, ownerID); err != nil {
		s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
		return err
	}
	s.setContainerDesiredStatus(ctx, containerID, model.ContainerStatusRunning)

	if err := s.dockerAPI.StartContainer(ctx, c.DockerID); err != nil {
		if isDockerNotFound(err) {
			s.markContainerError(ctx, containerID, model.ContainerStatusMissing, resourceMissingError("container"))
			s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
			return resourceUnavailableError("container")
		}
		s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
		return err
	}

	err = s.repo.UpdateStatus(ctx, containerID, model.ContainerStatusRunning)

	// Вызываем ребалансировку в фоне
	if err == nil {
		s.logger.InfoContext(ctx, "container started", "container_id", containerID, "owner_id", ownerID)
		s.completeContainerOperation(ctx, containerID, model.OperationStatusDone, nil)
		s.RequestRebalance()
		s.refreshProjectStatus(ctx, c.ProjectID)
	} else {
		s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
	}

	return err
}

func (s *ContainerService) Stop(ctx context.Context, containerID uuid.UUID) error {
	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return err
	}
	if err := accessscope.RequireOwnerAccess(ctx, c.OwnerID); err != nil {
		return err
	}
	ownerID := c.OwnerID
	if c.Status == model.ContainerStatusMissing {
		return resourceUnavailableError("container")
	}
	if c.DockerID == "" {
		s.markContainerError(ctx, containerID, model.ContainerStatusMissing, resourceMissingError("container"))
		return resourceUnavailableError("container")
	}
	s.setContainerDesiredStatus(ctx, containerID, model.ContainerStatusExited)
	s.createContainerOperation(ctx, containerID, ownerID, model.OperationStop)

	if err := s.dockerAPI.StopContainer(ctx, c.DockerID, s.config.Get().ContainerStopTimeout); err != nil {
		if !isDockerNotFound(err) {
			s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
			return err
		}
		s.markContainerError(ctx, containerID, model.ContainerStatusMissing, resourceMissingError("container"))
		s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
		return resourceUnavailableError("container")
	}

	err = s.repo.UpdateStatus(ctx, containerID, model.ContainerStatusExited)

	// Кто-то остановился -> освободились ресурсы -> ребалансируем остальных!
	if err == nil {
		s.logger.InfoContext(ctx, "container stopped", "container_id", containerID, "owner_id", ownerID)
		s.completeContainerOperation(ctx, containerID, model.OperationStatusDone, nil)
		s.RequestRebalance()
		s.refreshProjectStatus(ctx, c.ProjectID)
	} else {
		s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
	}

	return err
}

func (s *ContainerService) Delete(ctx context.Context, containerID uuid.UUID) error {
	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return err
	}
	if err := accessscope.RequireOwnerAccess(ctx, c.OwnerID); err != nil {
		return err
	}
	ownerID := c.OwnerID
	s.setContainerDesiredStatus(ctx, containerID, model.ContainerStatusDeleting)
	s.createContainerOperation(ctx, containerID, ownerID, model.OperationDelete)

	if c.DockerID != "" {
		err = s.dockerAPI.RemoveContainer(ctx, c.DockerID, true)
		if err != nil && !isDockerNotFound(err) {
			s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
			return err
		}
	}

	err = s.repo.Delete(ctx, containerID)
	if err != nil {
		s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
		return err
	}
	s.completeContainerOperation(ctx, containerID, model.OperationStatusDone, nil)

	s.cleanupUserNetworkIfUnused(ctx, ownerID)
	s.refreshProjectStatus(ctx, c.ProjectID)

	s.logger.InfoContext(ctx, "container deleted", "container_id", containerID, "owner_id", ownerID)
	return nil
}

func (s *ContainerService) CleanupUserNetworkIfUnused(ctx context.Context, ownerID uuid.UUID) error {
	count, err := s.repo.CountByOwnerID(ctx, ownerID)
	if err != nil {
		err = fmt.Errorf("failed to count user containers: %w", err)
		s.logger.WarnContext(ctx, "failed to cleanup user network", "owner_id", ownerID, "error", err)
		return err
	}
	if count > 0 {
		return nil
	}

	networkName := userNetworkName(ownerID)
	if err := s.dockerAPI.RemoveNetwork(ctx, networkName); err != nil {
		err = fmt.Errorf("failed to remove user network %s: %w", networkName, err)
		s.logger.WarnContext(ctx, "failed to cleanup user network", "owner_id", ownerID, "network", networkName, "error", err)
		return err
	}

	s.logger.InfoContext(ctx, "user network removed", "owner_id", ownerID, "network", networkName)
	return nil
}

func (s *ContainerService) cleanupUserNetworkIfUnused(ctx context.Context, ownerID uuid.UUID) {
	_ = s.CleanupUserNetworkIfUnused(ctx, ownerID)
}

func userNetworkName(ownerID uuid.UUID) string {
	return fmt.Sprintf("net_user_%s", ownerID.String())
}

func (s *ContainerService) List(ctx context.Context, limit, offset int) ([]model.Container, int, error) {
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

func (s *ContainerService) checkUserDiskQuota(ctx context.Context, ownerID uuid.UUID) error {
	imageRepo, ok := s.imageRepo.(containerImageDiskRepository)
	if !ok {
		return nil
	}
	user, err := s.users.GetUser(ctx, ownerID)
	if err != nil {
		return err
	}
	usedMB, err := imageRepo.GetUserUsedDiskSpace(ctx, ownerID)
	if err != nil {
		return err
	}
	if volumeRepo, ok := s.volumeRepo.(volumeDiskUsageRepository); ok {
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

func (s *ContainerService) checkHostCapacity(ctx context.Context, requestedRam int64) error {
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
	totalRunningReserved, err := s.repo.GetTotalSystemReservedMemory(ctx)
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
	cfg := s.config.Get()
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
	availableForBurst := totalMem - cfg.ReservedSystemMemory

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
		maxAllowedBurst := c.BaseMemoryReservation * cfg.MaxBurstMultiplier
		if newMemoryLimit > maxAllowedBurst {
			newMemoryLimit = maxAllowedBurst
		}

		// Выдаем CpuShares: если мало контейнеров - высокий приоритет, если много - стандартный
		cpuShares := cfg.DefaultCPUShares
		if len(runningContainers) > cfg.HighLoadContainerCount {
			cpuShares = cfg.HighLoadCPUShares
		}

		err := s.dockerAPI.UpdateContainerResources(ctx, c.DockerID, newMemoryLimit, c.BaseMemoryReservation, cpuShares, cfg.ContainerMemorySwapMultiplier)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to update container resources", "container_id", c.ID, "docker_id", c.DockerID, "error", err)
		}
	}
	s.logger.InfoContext(ctx, "containers rebalanced", "container_count", len(runningContainers), "burst_factor", burstFactor)
}

func (s *ContainerService) RequestRebalance() {
	select {
	case s.rebalanceCh <- struct{}{}:
	default:
	}
}

func (s *ContainerService) RunRebalancer(ctx context.Context) {
	s.logger.InfoContext(ctx, "resource rebalancer started")
	for {
		select {
		case <-ctx.Done():
			s.logger.InfoContext(ctx, "resource rebalancer stopped")
			return
		case <-s.rebalanceCh:
			s.RebalanceResources(ctx)
		}
	}
}

func (s *ContainerService) acquireOwnerCapacityLock(ctx context.Context, ownerID uuid.UUID) (func(), error) {
	lockRepo, ok := s.repo.(containerLockRepository)
	if !ok {
		return nil, nil
	}
	return lockRepo.AcquireOwnerCapacityLock(ctx, ownerID)
}

func (s *ContainerService) setContainerDesiredStatus(ctx context.Context, containerID uuid.UUID, status string) {
	stateRepo, ok := s.repo.(containerStateRepository)
	if !ok {
		return
	}
	if err := stateRepo.SetDesiredStatus(ctx, containerID, status); err != nil {
		s.logger.WarnContext(ctx, "failed to set desired container status", "container_id", containerID, "desired_status", status, "error", err)
	}
}

func (s *ContainerService) markContainerError(ctx context.Context, containerID uuid.UUID, status string, cause error) {
	stateRepo, ok := s.repo.(containerStateRepository)
	if !ok {
		return
	}
	if err := stateRepo.MarkStatusError(ctx, containerID, status, cause); err != nil {
		s.logger.WarnContext(ctx, "failed to mark container error", "container_id", containerID, "status", status, "error", err)
	}
}

func (s *ContainerService) markVolumeMissing(ctx context.Context, volumeID uuid.UUID) {
	stateRepo, ok := s.volumeRepo.(volumeStateRepository)
	if !ok {
		return
	}
	if err := stateRepo.MarkStatusError(ctx, volumeID, model.VolumeStatusMissing, resourceMissingError("volume")); err != nil {
		s.logger.WarnContext(ctx, "failed to mark volume missing", "volume_id", volumeID, "error", err)
	}
}

func (s *ContainerService) markImageMissing(ctx context.Context, imageID uuid.UUID) {
	stateRepo, ok := s.imageRepo.(imageStateRepository)
	if !ok {
		return
	}
	if err := stateRepo.MarkStatusError(ctx, imageID, model.ImageStatusMissing, resourceMissingError("image")); err != nil {
		s.logger.WarnContext(ctx, "failed to mark image missing", "image_id", imageID, "error", err)
	}
}

func (s *ContainerService) createContainerOperation(ctx context.Context, containerID, ownerID uuid.UUID, operation string) {
	stateRepo, ok := s.repo.(containerStateRepository)
	if !ok {
		return
	}
	op := model.ResourceOperation{
		ID:           uuid.New(),
		ResourceType: model.ResourceTypeContainer,
		ResourceID:   containerID,
		OwnerID:      ownerID,
		Operation:    operation,
		Status:       model.OperationStatusRunning,
	}
	if err := stateRepo.CreateOperation(ctx, op); err != nil {
		s.logger.WarnContext(ctx, "failed to create resource operation", "container_id", containerID, "operation", operation, "error", err)
	}
}

func (s *ContainerService) completeContainerOperation(ctx context.Context, containerID uuid.UUID, status string, cause error) {
	stateRepo, ok := s.repo.(containerStateRepository)
	if !ok {
		return
	}
	if err := stateRepo.CompleteLatestOperation(ctx, model.ResourceTypeContainer, containerID, status, cause); err != nil {
		s.logger.WarnContext(ctx, "failed to complete resource operation", "container_id", containerID, "status", status, "error", err)
	}
}

func isDockerNotFound(err error) bool {
	return err != nil && cerrdefs.IsNotFound(err)
}

func resourceUnavailableError(resource string) error {
	if resource == "" {
		resource = "resource"
	}
	return apperrors.New(apperrors.ErrConflict, resource+" is missing in Docker and can only be deleted")
}

func (s *ContainerService) Action(ctx context.Context, containerID uuid.UUID, action string) error {
	switch action {
	case "start":
		return s.Start(ctx, containerID)
	case "stop":
		return s.Stop(ctx, containerID)
	case "delete":
		return s.Delete(ctx, containerID)
	default:
		return fmt.Errorf("%w: invalid container action", apperrors.ErrBadRequest)
	}
}

func (s *ContainerService) refreshProjectStatus(ctx context.Context, projectID *uuid.UUID) {
	if s.projects == nil || projectID == nil {
		return
	}
	if err := s.projects.RefreshProjectStatus(ctx, *projectID); err != nil {
		s.logger.WarnContext(ctx, "failed to refresh project status", "project_id", *projectID, "error", err)
	}
}

func (s *ContainerService) GetStats(ctx context.Context, containerID uuid.UUID) (model.ContainerStats, error) {
	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return model.ContainerStats{}, err
	}
	if err := accessscope.RequireOwnerAccess(ctx, c.OwnerID); err != nil {
		return model.ContainerStats{}, err
	}
	if c.Status == model.ContainerStatusMissing {
		return model.ContainerStats{}, resourceUnavailableError("container")
	}
	if c.Status != model.ContainerStatusRunning {
		return model.ContainerStats{}, nil
	}
	stats, err := s.dockerAPI.GetContainerStats(ctx, c.DockerID)
	if err != nil && isDockerNotFound(err) {
		s.markContainerError(ctx, containerID, model.ContainerStatusMissing, resourceMissingError("container"))
		return model.ContainerStats{}, resourceUnavailableError("container")
	}
	return stats, err
}

func (s *ContainerService) GetRuntimeTarget(ctx context.Context, containerID uuid.UUID) (model.ContainerRuntimeTarget, error) {
	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return model.ContainerRuntimeTarget{}, err
	}
	if err := accessscope.RequireOwnerAccess(ctx, c.OwnerID); err != nil {
		return model.ContainerRuntimeTarget{}, err
	}
	if c.Status == model.ContainerStatusMissing {
		return model.ContainerRuntimeTarget{}, resourceUnavailableError("container")
	}
	if c.DockerID == "" {
		return model.ContainerRuntimeTarget{}, apperrors.ErrNotFound
	}
	return model.ContainerRuntimeTarget{
		ContainerID:      c.ID,
		DockerID:         c.DockerID,
		Status:           c.Status,
		OwnerID:          c.OwnerID,
		DockerGeneration: c.DockerGeneration,
	}, nil
}
