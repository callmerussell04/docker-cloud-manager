package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
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
	CreateOperationAndSetDesired(ctx context.Context, id uuid.UUID, desiredStatus string, op model.ResourceOperation) error
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

type containerImageRemover interface {
	RemoveImage(ctx context.Context, imageID string, force bool) error
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
	repo         ContainerRepository
	volumeRepo   ContainerVolumeRepository
	imageRepo    ContainerImageRepository
	dockerAPI    ContainerDockerAPI
	metrics      HostMetricsProvider
	config       ConfigManager
	users        UserInfoProvider
	projects     ProjectStatusUpdater
	auditor      AuditRecorder
	hostDiskPath string
	logger       *slog.Logger
	rebalanceCh  chan struct{}
}

func NewContainerService(
	repo ContainerRepository,
	volumeRepo ContainerVolumeRepository,
	imageRepo ContainerImageRepository,
	dockerAPI ContainerDockerAPI,
	metrics HostMetricsProvider,
	config ConfigManager,
	users UserInfoProvider,
	hostDiskPath string,
	logger *slog.Logger,
) *ContainerService {
	return &ContainerService{
		repo:         repo,
		volumeRepo:   volumeRepo,
		imageRepo:    imageRepo,
		dockerAPI:    dockerAPI,
		metrics:      metrics,
		config:       config,
		users:        users,
		hostDiskPath: hostDiskPath,
		logger:       logging.WithComponent(logger, "container_service"),
		rebalanceCh:  make(chan struct{}, 1),
	}
}

func (s *ContainerService) SetProjectStatusUpdater(updater ProjectStatusUpdater) {
	s.projects = updater
}

func (s *ContainerService) SetAuditRecorder(auditor AuditRecorder) {
	s.auditor = auditor
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
	releaseLock := func() {
		if unlock != nil {
			unlock()
			unlock = nil
		}
	}
	defer releaseLock()

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
	if err := s.checkHostDiskCapacity(); err != nil {
		return uuid.Nil, err
	}

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
		actualImageTag = customImageFullTag(s.config.Get().RegistryPublicURL, ownerID, baseName, version)
	}

	envBytes, err := json.Marshal(params.EnvVars)
	if err != nil {
		return uuid.Nil, err
	}

	containerID := uuid.New()

	// Подготовка томов
	var dockerMounts []model.ContainerMountSpec
	var dbMounts []model.VolumeMount
	var volumeChecks []struct {
		id         uuid.UUID
		dockerName string
	}

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
		volumeChecks = append(volumeChecks, struct {
			id         uuid.UUID
			dockerName string
		}{id: vol.ID, dockerName: vol.DockerName})

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

	networkAlias := params.NetworkAlias
	if networkAlias == "" {
		networkAlias = params.Name
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
		NetworkAlias:          networkAlias,
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
	releaseLock()

	if inspector, ok := s.dockerAPI.(containerVolumeInspector); ok {
		for _, check := range volumeChecks {
			if _, err := inspector.InspectVolume(ctx, check.dockerName); err != nil {
				if isDockerNotFound(err) {
					s.markVolumeMissing(ctx, check.id)
					err = resourceUnavailableError("volume")
				}
				s.markContainerError(ctx, containerID, model.ContainerStatusError, err)
				s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
				return uuid.Nil, err
			}
		}
	}

	// 4. Изоляция сети
	networkName := userNetworkName(ownerID)
	_, err = s.dockerAPI.EnsureUserNetwork(ctx, networkName)
	if err != nil {
		s.markContainerError(ctx, containerID, model.ContainerStatusError, err)
		s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
		return uuid.Nil, err
	}
	defer func() {
		if err != nil {
			s.cleanupUserNetworkIfUnused(ctx, ownerID)
		}
	}()

	// Если образ кастомный — ПУЛЛИМ ВСЕГДА (вдруг пользователь пересобрал его)
	// Если публичный — пуллим только если его нет на хосте
	pulledPublicImage := false
	defer func() {
		if err != nil && pulledPublicImage {
			cleanupCtx, cancel := detachedCleanupContext(ctx)
			if remover, ok := s.dockerAPI.(containerImageRemover); ok {
				_ = remover.RemoveImage(cleanupCtx, actualImageTag, false)
			}
			cancel()
		}
	}()
	if isCustom {
		err = s.dockerAPI.PullImage(ctx, actualImageTag)
		if err != nil {
			if customImage != nil && isDockerNotFound(err) {
				s.markImageMissing(ctx, customImage.ID)
			}
			err = normalizeContainerRuntimeError(err)
			if !errors.Is(err, apperrors.ErrTimeout) {
				err = apperrors.Wrap(apperrors.ErrBadRequest, "image could not be pulled", err)
			}
			s.markContainerError(ctx, containerID, model.ContainerStatusError, err)
			s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
			return uuid.Nil, err
		}
	} else {
		imageExists, imageErr := s.dockerAPI.ImageExists(ctx, actualImageTag)
		if imageErr != nil {
			err = imageErr
			s.markContainerError(ctx, containerID, model.ContainerStatusError, err)
			s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
			return uuid.Nil, err
		}
		if !imageExists {
			err = s.checkHostDiskCapacity()
			if err != nil {
				s.markContainerError(ctx, containerID, model.ContainerStatusError, err)
				s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
				return uuid.Nil, err
			}
			err = s.dockerAPI.PullImage(ctx, actualImageTag)
			if err != nil {
				err = normalizeContainerRuntimeError(err)
				if !errors.Is(err, apperrors.ErrTimeout) {
					err = apperrors.Wrap(apperrors.ErrBadRequest, "image could not be pulled", err)
				}
				s.markContainerError(ctx, containerID, model.ContainerStatusError, err)
				s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
				return uuid.Nil, err
			}
			pulledPublicImage = true
		}
	}

	// 5. Конфигурация Docker. Изначально ставим жесткий лимит равным мягкому.
	// Ребалансировщик потом его увеличит (Burst).
	dockerParams := model.ContainerRuntimeSpec{
		ContainerID:          containerID.String(),
		OwnerID:              ownerID.String(),
		Generation:           c.DockerGeneration,
		ContainerName:        fmt.Sprintf("usr_%s", containerID.String()[:12]),
		NetworkAlias:         networkAlias,
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
		err = normalizeContainerRuntimeError(err)
		s.markContainerError(ctx, containerID, model.ContainerStatusError, err)
		s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
		return uuid.Nil, err
	}

	err = s.repo.UpdateDockerIDAndStatus(ctx, containerID, dockerID, model.ContainerStatusCreated)
	if err != nil {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		s.dockerAPI.RemoveContainer(cleanupCtx, dockerID, true)
		s.markContainerError(cleanupCtx, containerID, model.ContainerStatusError, err)
		s.completeContainerOperation(cleanupCtx, containerID, model.OperationStatusFailed, err)
		cancel()
		return uuid.Nil, err
	}

	s.completeContainerOperation(ctx, containerID, model.OperationStatusDone, nil)
	s.logger.InfoContext(ctx, "container created", "container_id", containerID, "owner_id", ownerID, "image_tag", normalizedInputTag)
	return containerID, nil
}

func (s *ContainerService) Expose(ctx context.Context, containerID uuid.UUID, domainPrefix string, internalPort int) (err error) {
	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return err
	}
	if err := accessscope.RequireOwnerAccess(ctx, c.OwnerID); err != nil {
		return err
	}
	defer func() {
		s.recordExposeAudit(ctx, c, domainPrefix, internalPort, err)
	}()
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

	networkAlias := c.NetworkAlias
	if networkAlias == "" {
		networkAlias = c.Name
	}

	dockerParams := model.ContainerRuntimeSpec{
		ContainerID:          containerID.String(),
		OwnerID:              ownerID.String(),
		Generation:           nextGeneration,
		ContainerName:        fmt.Sprintf("%s_g%d", strings.TrimPrefix(inspect.Name, "/"), nextGeneration),
		NetworkAlias:         networkAlias,
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

	if err := s.createContainerOperation(ctx, containerID, ownerID, model.OperationExpose); err != nil {
		return err
	}
	if err := s.checkHostDiskCapacity(); err != nil {
		s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
		return err
	}

	// Создаем новый контейнер с лейблами Traefik до удаления старого.
	newDockerID, err := s.dockerAPI.CreateContainer(ctx, dockerParams)
	if err != nil {
		err = normalizeContainerRuntimeError(err)
		s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
		return err
	}
	newStarted := false
	defer func() {
		if err != nil {
			cleanupCtx, cancel := detachedCleanupContext(ctx)
			_ = s.dockerAPI.RemoveContainer(cleanupCtx, newDockerID, true)
			cancel()
		}
	}()

	if c.Status == model.ContainerStatusRunning {
		if err = s.dockerAPI.StartContainer(ctx, newDockerID); err != nil {
			err = normalizeContainerRuntimeError(err)
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
			cleanupCtx, cancel := detachedCleanupContext(ctx)
			_ = s.dockerAPI.StopContainer(cleanupCtx, newDockerID, s.config.Get().ContainerStopTimeout)
			cancel()
		}
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		s.completeContainerOperation(cleanupCtx, containerID, model.OperationStatusFailed, err)
		cancel()
		return err
	}

	if c.DockerID != "" {
		if removeErr := s.dockerAPI.RemoveContainer(ctx, c.DockerID, true); removeErr != nil && !isDockerNotFound(removeErr) {
			removeErr = normalizeContainerRuntimeError(removeErr)
			s.logger.WarnContext(ctx, "failed to remove old exposed container generation", "container_id", containerID, "docker_id", c.DockerID, "error", removeErr)
		}
	}

	s.completeContainerOperation(ctx, containerID, model.OperationStatusDone, nil)
	err = nil
	return nil
}

func (s *ContainerService) recordExposeAudit(ctx context.Context, c model.Container, domainPrefix string, internalPort int, opErr error) {
	if s.auditor == nil {
		return
	}
	outcome := auditlog.OutcomeSuccess
	errorCode := ""
	if opErr != nil {
		outcome = auditlog.OutcomeFailure
		errorCode = apperrors.SafeMessage(opErr)
	}
	actorID, actorUsername, actorScope := auditActorFromContext(ctx)
	ownerUsername := c.OwnerUsername
	if ownerUsername == "" && c.OwnerID == actorIDValue(actorID) {
		ownerUsername = actorUsername
	}
	details := map[string]string{
		auditlog.DetailSourceType:           "manual",
		auditlog.DetailContainerID:          c.ID.String(),
		auditlog.DetailContainerName:        c.Name,
		auditlog.DetailDomainPrefix:         domainPrefix,
		auditlog.DetailFullDomain:           s.fullDomain(domainPrefix),
		auditlog.DetailInternalPort:         auditlog.IntDetail(internalPort),
		auditlog.DetailPreviousDomainPrefix: c.DomainPrefix,
		auditlog.DetailPreviousInternalPort: auditlog.IntDetail(c.InternalPort),
	}
	if c.ProjectID != nil {
		details[auditlog.DetailProjectID] = c.ProjectID.String()
	}
	_ = s.auditor.RecordAuditEvent(ctx, model.AuditEvent{
		ActorUserID:   actorID,
		ActorUsername: actorUsername,
		ActorScope:    actorScope,
		Action:        auditlog.ActionContainerExpose,
		Outcome:       outcome,
		ResourceType:  auditlog.ResourceContainer,
		ResourceID:    c.ID.String(),
		ResourceName:  c.Name,
		OwnerID:       ownerPtr(c.OwnerID),
		OwnerUsername: ownerUsername,
		RequestID:     requestIDFromContext(ctx),
		ErrorCode:     errorCode,
		DetailsJSON:   auditlog.SafeDetailsJSON(details),
	})
}

func (s *ContainerService) fullDomain(domainPrefix string) string {
	if domainPrefix == "" || s.config == nil {
		return ""
	}
	baseDomain := s.config.Get().BaseDomain
	if baseDomain == "" {
		return domainPrefix
	}
	return fmt.Sprintf("%s.%s", domainPrefix, baseDomain)
}

func actorIDValue(actorID *uuid.UUID) uuid.UUID {
	if actorID == nil {
		return uuid.Nil
	}
	return *actorID
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
	releaseLock := func() {
		if unlock != nil {
			unlock()
			unlock = nil
		}
	}
	defer releaseLock()
	// Повторная проверка перед стартом (вдруг пока он был 'exited', студент запустил другие)
	if err := s.checkUserQuota(ctx, ownerID, c.BaseMemoryReservation); err != nil {
		return err
	}
	if err := s.checkHostCapacity(ctx, c.BaseMemoryReservation); err != nil {
		return err
	}
	if err := s.checkUserDiskQuota(ctx, ownerID); err != nil {
		return err
	}
	if err := s.checkHostDiskCapacity(); err != nil {
		return err
	}
	if err := s.createContainerOperationAndSetDesired(ctx, containerID, ownerID, model.OperationStart, model.ContainerStatusRunning); err != nil {
		return err
	}
	releaseLock()

	if err := s.dockerAPI.StartContainer(ctx, c.DockerID); err != nil {
		if isDockerNotFound(err) {
			s.markContainerError(ctx, containerID, model.ContainerStatusMissing, resourceMissingError("container"))
			s.completeContainerOperation(ctx, containerID, model.OperationStatusFailed, err)
			return resourceUnavailableError("container")
		}
		err = normalizeContainerRuntimeError(err)
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
	if err := s.createContainerOperationAndSetDesired(ctx, containerID, ownerID, model.OperationStop, model.ContainerStatusExited); err != nil {
		return err
	}

	if err := s.dockerAPI.StopContainer(ctx, c.DockerID, s.config.Get().ContainerStopTimeout); err != nil {
		if !isDockerNotFound(err) {
			err = normalizeContainerRuntimeError(err)
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
	if err := s.createContainerOperationAndSetDesired(ctx, containerID, ownerID, model.OperationDelete, model.ContainerStatusDeleting); err != nil {
		return err
	}

	if c.DockerID != "" {
		err = s.dockerAPI.RemoveContainer(ctx, c.DockerID, true)
		if err != nil && !isDockerNotFound(err) {
			err = normalizeContainerRuntimeError(err)
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
