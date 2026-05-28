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
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/containerqueue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/google/uuid"
)

type containerCreateWorkerRepository interface {
	GetOperationByID(ctx context.Context, id uuid.UUID) (model.ResourceOperation, error)
	ClaimPendingOperation(ctx context.Context, id uuid.UUID, maxAttempts int) (model.ResourceOperation, bool, error)
	CompleteOperation(ctx context.Context, id uuid.UUID, status string, cause error) error
	RequeueOperation(ctx context.Context, id uuid.UUID, cause error) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Container, error)
	Delete(ctx context.Context, id uuid.UUID) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	UpdateDockerIDAndStatus(ctx context.Context, id uuid.UUID, dockerID string, status string) error
	UpdateDockerIDRoutingAndGeneration(ctx context.Context, id uuid.UUID, dockerID string, domainPrefix string, internalPort int, generation int) error
	MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error
	GetMountsByContainerID(ctx context.Context, containerID uuid.UUID) ([]model.VolumeMountParams, error)
}

type ContainerLifecycleConsumer interface {
	Run(ctx context.Context, workers int, handler func(context.Context, containerqueue.LifecycleMessage) error)
	Stop(ctx context.Context) error
}

type ContainerCreateWorker struct {
	service *ContainerService
	cfg     ContainerCreateConfigProvider
	logger  *slog.Logger
}

type ContainerCreateConfigProvider interface {
	Get() config.SystemConfig
}

func NewContainerCreateWorker(service *ContainerService, cfg ContainerCreateConfigProvider, logger *slog.Logger) *ContainerCreateWorker {
	return &ContainerCreateWorker{
		service: service,
		cfg:     cfg,
		logger:  logging.WithComponent(logger, "container_create_worker"),
	}
}

func (w *ContainerCreateWorker) HandleMessage(ctx context.Context, msg containerqueue.LifecycleMessage) error {
	operationID, err := uuid.Parse(msg.OperationID)
	if err != nil {
		return apperrors.New(apperrors.ErrBadRequest, "invalid container operation id")
	}
	containerID, err := uuid.Parse(msg.ContainerID)
	if err != nil {
		return apperrors.New(apperrors.ErrBadRequest, "invalid container id")
	}
	timeout := time.Duration(w.cfg.Get().ContainerCreateTimeoutMinutes) * time.Minute
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return w.service.ExecuteQueuedContainerOperation(runCtx, operationID, containerID, msg)
}

func (s *ContainerService) ExecuteQueuedContainerOperation(ctx context.Context, operationID, containerID uuid.UUID, msg containerqueue.LifecycleMessage) error {
	repo, ok := s.repo.(containerCreateWorkerRepository)
	if !ok {
		return apperrors.New(apperrors.ErrUnavailable, "container lifecycle queue repository is unavailable")
	}
	op, err := repo.GetOperationByID(ctx, operationID)
	if err != nil {
		return err
	}
	if op.Operation == model.OperationCreate {
		return s.ExecuteQueuedCreate(ctx, operationID, containerID)
	}
	return s.executeQueuedLifecycle(ctx, repo, op, containerID, msg)
}

func (s *ContainerService) ExecuteQueuedCreate(ctx context.Context, operationID, containerID uuid.UUID) (err error) {
	repo, ok := s.repo.(containerCreateWorkerRepository)
	if !ok {
		return apperrors.New(apperrors.ErrUnavailable, "container create queue repository is unavailable")
	}
	cfg := s.config.Get()
	op, claimed, err := repo.ClaimPendingOperation(ctx, operationID, cfg.ContainerCreateMaxAttempts)
	if err != nil {
		return err
	}
	if !claimed {
		if op.Status == model.OperationStatusPending && cfg.ContainerCreateMaxAttempts > 0 && op.Attempts >= cfg.ContainerCreateMaxAttempts {
			cause := apperrors.New(apperrors.ErrTimeout, "container create attempts exhausted")
			s.failQueuedCreate(ctx, repo, operationID, containerID, cause, false)
		}
		return nil
	}
	if op.ResourceType != model.ResourceTypeContainer || op.Operation != model.OperationCreate || op.ResourceID != containerID {
		cause := apperrors.New(apperrors.ErrBadRequest, "container create message does not match operation")
		_ = repo.CompleteOperation(ctx, operationID, model.OperationStatusFailed, cause)
		return cause
	}

	if err := repo.UpdateStatus(ctx, containerID, model.ContainerStatusCreating); err != nil {
		_ = repo.RequeueOperation(context.WithoutCancel(ctx), operationID, err)
		return err
	}

	c, err := repo.GetByID(ctx, containerID)
	if err != nil {
		_ = repo.CompleteOperation(context.WithoutCancel(ctx), operationID, model.OperationStatusFailed, err)
		return err
	}
	if c.DockerID != "" {
		_ = repo.CompleteOperation(context.WithoutCancel(ctx), operationID, model.OperationStatusDone, nil)
		return nil
	}

	mounts, err := repo.GetMountsByContainerID(ctx, containerID)
	if err != nil {
		s.failQueuedCreate(ctx, repo, operationID, containerID, err, false)
		return err
	}
	if err := s.ensureCreateOperationRunning(ctx, repo, operationID); err != nil {
		return err
	}
	if err := s.inspectQueuedCreateVolumes(ctx, mounts); err != nil {
		s.failQueuedCreate(ctx, repo, operationID, containerID, err, false)
		return err
	}

	networkName := userNetworkName(c.OwnerID)
	if _, err = s.dockerAPI.EnsureUserNetwork(ctx, networkName); err != nil {
		err = normalizeContainerRuntimeError(err)
		if s.requeueQueuedCreateIfRetryable(ctx, repo, operationID, err) {
			return err
		}
		s.failQueuedCreate(ctx, repo, operationID, containerID, err, false)
		return err
	}
	defer func() {
		if err != nil {
			s.cleanupUserNetworkIfUnused(ctx, c.OwnerID)
		}
	}()

	actualImageTag, customImage, isCustom, err := s.resolveQueuedCreateImage(ctx, c)
	if err != nil {
		s.failQueuedCreate(ctx, repo, operationID, containerID, err, false)
		return err
	}
	if err := s.ensureCreateOperationRunning(ctx, repo, operationID); err != nil {
		return err
	}

	pulledPublicImage := false
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
			if s.requeueQueuedCreateIfRetryable(ctx, repo, operationID, err) {
				return err
			}
			s.failQueuedCreate(ctx, repo, operationID, containerID, err, false)
			return err
		}
	} else {
		imageExists, imageErr := s.dockerAPI.ImageExists(ctx, actualImageTag)
		if imageErr != nil {
			err = normalizeContainerRuntimeError(imageErr)
			if s.requeueQueuedCreateIfRetryable(ctx, repo, operationID, err) {
				return err
			}
			s.failQueuedCreate(ctx, repo, operationID, containerID, err, false)
			return err
		}
		if !imageExists {
			if err = s.checkHostDiskCapacity(); err != nil {
				s.failQueuedCreate(ctx, repo, operationID, containerID, err, false)
				return err
			}
			err = s.dockerAPI.PullImage(ctx, actualImageTag)
			if err != nil {
				err = normalizeContainerRuntimeError(err)
				if !errors.Is(err, apperrors.ErrTimeout) {
					err = apperrors.Wrap(apperrors.ErrBadRequest, "image could not be pulled", err)
				}
				if s.requeueQueuedCreateIfRetryable(ctx, repo, operationID, err) {
					return err
				}
				s.failQueuedCreate(ctx, repo, operationID, containerID, err, false)
				return err
			}
			pulledPublicImage = true
		}
	}
	defer func() {
		if err != nil && pulledPublicImage {
			cleanupCtx, cancel := detachedCleanupContext(ctx)
			if remover, ok := s.dockerAPI.(containerImageRemover); ok {
				_ = remover.RemoveImage(cleanupCtx, actualImageTag, false)
			}
			cancel()
		}
	}()

	if err := s.ensureCreateOperationRunning(ctx, repo, operationID); err != nil {
		return err
	}
	dockerParams, err := s.queuedCreateRuntimeSpec(c, mounts, actualImageTag, networkName)
	if err != nil {
		s.failQueuedCreate(ctx, repo, operationID, containerID, err, false)
		return err
	}
	dockerID, err := s.dockerAPI.CreateContainer(ctx, dockerParams)
	if err != nil {
		err = normalizeContainerRuntimeError(err)
		if s.requeueQueuedCreateIfRetryable(ctx, repo, operationID, err) {
			return err
		}
		s.failQueuedCreate(ctx, repo, operationID, containerID, err, false)
		return err
	}

	if err = repo.UpdateDockerIDAndStatus(ctx, containerID, dockerID, model.ContainerStatusCreated); err != nil {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		_ = s.dockerAPI.RemoveContainer(cleanupCtx, dockerID, true)
		cancel()
		s.failQueuedCreate(ctx, repo, operationID, containerID, err, true)
		return err
	}
	if err := repo.CompleteOperation(ctx, operationID, model.OperationStatusDone, nil); err != nil {
		s.logger.WarnContext(ctx, "failed to complete container create operation", "container_id", containerID, "operation_id", operationID, "error", err)
	}
	s.logger.InfoContext(ctx, "container created", "container_id", containerID, "owner_id", c.OwnerID, "image_tag", c.ImageTag)
	return nil
}

func (s *ContainerService) executeQueuedLifecycle(ctx context.Context, repo containerCreateWorkerRepository, op model.ResourceOperation, containerID uuid.UUID, msg containerqueue.LifecycleMessage) error {
	cfg := s.config.Get()
	claimedOp, claimed, err := repo.ClaimPendingOperation(ctx, op.ID, cfg.ContainerCreateMaxAttempts)
	if err != nil {
		return err
	}
	if !claimed {
		if claimedOp.Status == model.OperationStatusPending && cfg.ContainerCreateMaxAttempts > 0 && claimedOp.Attempts >= cfg.ContainerCreateMaxAttempts {
			cause := apperrors.New(apperrors.ErrTimeout, "container lifecycle attempts exhausted")
			s.failQueuedLifecycle(ctx, repo, op.ID, containerID, msg.PreviousStatus, cause)
		}
		return nil
	}
	if claimedOp.ResourceType != model.ResourceTypeContainer || claimedOp.ResourceID != containerID {
		cause := apperrors.New(apperrors.ErrBadRequest, "container lifecycle message does not match operation")
		_ = repo.CompleteOperation(context.WithoutCancel(ctx), op.ID, model.OperationStatusFailed, cause)
		return cause
	}

	switch claimedOp.Operation {
	case model.OperationStart:
		return s.executeQueuedStart(ctx, repo, claimedOp, containerID, msg)
	case model.OperationStop:
		return s.executeQueuedStop(ctx, repo, claimedOp, containerID, msg)
	case model.OperationDelete:
		return s.executeQueuedDelete(ctx, repo, claimedOp, containerID, msg)
	case model.OperationExpose:
		return s.executeQueuedExpose(ctx, repo, claimedOp, containerID, msg)
	default:
		cause := apperrors.New(apperrors.ErrBadRequest, "unsupported container lifecycle operation")
		s.failQueuedLifecycle(ctx, repo, op.ID, containerID, msg.PreviousStatus, cause)
		return cause
	}
}

func (s *ContainerService) executeQueuedStart(ctx context.Context, repo containerCreateWorkerRepository, op model.ResourceOperation, containerID uuid.UUID, msg containerqueue.LifecycleMessage) error {
	c, err := repo.GetByID(ctx, containerID)
	if err != nil {
		_ = repo.CompleteOperation(context.WithoutCancel(ctx), op.ID, model.OperationStatusFailed, err)
		return err
	}
	if c.DockerID == "" {
		cause := resourceUnavailableError("container")
		s.failQueuedLifecycle(ctx, repo, op.ID, containerID, msg.PreviousStatus, cause)
		return cause
	}
	if err := s.ensureCreateOperationRunning(ctx, repo, op.ID); err != nil {
		return err
	}
	if err := s.dockerAPI.StartContainer(ctx, c.DockerID); err != nil {
		if isDockerNotFound(err) {
			s.markContainerError(ctx, containerID, model.ContainerStatusMissing, resourceMissingError("container"))
			_ = repo.CompleteOperation(context.WithoutCancel(ctx), op.ID, model.OperationStatusFailed, err)
			return resourceUnavailableError("container")
		}
		err = normalizeContainerRuntimeError(err)
		if s.requeueQueuedCreateIfRetryable(ctx, repo, op.ID, err) {
			return err
		}
		s.failQueuedLifecycle(ctx, repo, op.ID, containerID, msg.PreviousStatus, err)
		return err
	}
	if err := repo.UpdateStatus(ctx, containerID, model.ContainerStatusRunning); err != nil {
		s.failQueuedLifecycle(ctx, repo, op.ID, containerID, msg.PreviousStatus, err)
		return err
	}
	_ = repo.CompleteOperation(context.WithoutCancel(ctx), op.ID, model.OperationStatusDone, nil)
	s.logger.InfoContext(ctx, "container started", "container_id", containerID, "owner_id", c.OwnerID)
	s.RequestRebalance()
	s.refreshProjectStatus(ctx, c.ProjectID)
	return nil
}

func (s *ContainerService) executeQueuedStop(ctx context.Context, repo containerCreateWorkerRepository, op model.ResourceOperation, containerID uuid.UUID, msg containerqueue.LifecycleMessage) error {
	c, err := repo.GetByID(ctx, containerID)
	if err != nil {
		_ = repo.CompleteOperation(context.WithoutCancel(ctx), op.ID, model.OperationStatusFailed, err)
		return err
	}
	if c.DockerID == "" {
		cause := resourceUnavailableError("container")
		s.failQueuedLifecycle(ctx, repo, op.ID, containerID, msg.PreviousStatus, cause)
		return cause
	}
	if err := s.ensureCreateOperationRunning(ctx, repo, op.ID); err != nil {
		return err
	}
	if err := s.dockerAPI.StopContainer(ctx, c.DockerID, s.config.Get().ContainerStopTimeout); err != nil {
		if isDockerNotFound(err) {
			s.markContainerError(ctx, containerID, model.ContainerStatusMissing, resourceMissingError("container"))
			_ = repo.CompleteOperation(context.WithoutCancel(ctx), op.ID, model.OperationStatusFailed, err)
			return resourceUnavailableError("container")
		}
		err = normalizeContainerRuntimeError(err)
		if s.requeueQueuedCreateIfRetryable(ctx, repo, op.ID, err) {
			return err
		}
		s.failQueuedLifecycle(ctx, repo, op.ID, containerID, msg.PreviousStatus, err)
		return err
	}
	if err := repo.UpdateStatus(ctx, containerID, model.ContainerStatusExited); err != nil {
		s.failQueuedLifecycle(ctx, repo, op.ID, containerID, msg.PreviousStatus, err)
		return err
	}
	_ = repo.CompleteOperation(context.WithoutCancel(ctx), op.ID, model.OperationStatusDone, nil)
	s.logger.InfoContext(ctx, "container stopped", "container_id", containerID, "owner_id", c.OwnerID)
	s.RequestRebalance()
	s.refreshProjectStatus(ctx, c.ProjectID)
	return nil
}

func (s *ContainerService) executeQueuedDelete(ctx context.Context, repo containerCreateWorkerRepository, op model.ResourceOperation, containerID uuid.UUID, msg containerqueue.LifecycleMessage) error {
	c, err := repo.GetByID(ctx, containerID)
	if err != nil {
		_ = repo.CompleteOperation(context.WithoutCancel(ctx), op.ID, model.OperationStatusFailed, err)
		return err
	}
	if err := s.ensureCreateOperationRunning(ctx, repo, op.ID); err != nil {
		return err
	}
	if c.DockerID != "" {
		err = s.dockerAPI.RemoveContainer(ctx, c.DockerID, true)
		if err != nil && !isDockerNotFound(err) {
			err = normalizeContainerRuntimeError(err)
			if s.requeueQueuedCreateIfRetryable(ctx, repo, op.ID, err) {
				return err
			}
			s.failQueuedLifecycle(ctx, repo, op.ID, containerID, msg.PreviousStatus, err)
			return err
		}
	}
	if err := repo.Delete(ctx, containerID); err != nil {
		s.failQueuedLifecycle(ctx, repo, op.ID, containerID, msg.PreviousStatus, err)
		return err
	}
	_ = repo.CompleteOperation(context.WithoutCancel(ctx), op.ID, model.OperationStatusDone, nil)
	s.cleanupUserNetworkIfUnused(ctx, c.OwnerID)
	s.refreshProjectStatus(ctx, c.ProjectID)
	s.logger.InfoContext(ctx, "container deleted", "container_id", containerID, "owner_id", c.OwnerID)
	return nil
}

func (s *ContainerService) executeQueuedExpose(ctx context.Context, repo containerCreateWorkerRepository, op model.ResourceOperation, containerID uuid.UUID, msg containerqueue.LifecycleMessage) (err error) {
	c, err := repo.GetByID(ctx, containerID)
	if err != nil {
		_ = repo.CompleteOperation(context.WithoutCancel(ctx), op.ID, model.OperationStatusFailed, err)
		return err
	}
	if c.DockerID == "" {
		cause := resourceUnavailableError("container")
		s.failQueuedLifecycle(ctx, repo, op.ID, containerID, msg.PreviousStatus, cause)
		return cause
	}
	if msg.DomainPrefix == "" || msg.InternalPort <= 0 {
		cause := apperrors.New(apperrors.ErrBadRequest, "domain prefix and internal port are required")
		s.failQueuedLifecycle(ctx, repo, op.ID, containerID, msg.PreviousStatus, cause)
		return cause
	}
	if err := s.ensureCreateOperationRunning(ctx, repo, op.ID); err != nil {
		return err
	}
	inspect, err := s.dockerAPI.InspectContainer(ctx, c.DockerID)
	if err != nil {
		if isDockerNotFound(err) {
			s.markContainerError(ctx, containerID, model.ContainerStatusMissing, resourceMissingError("container"))
			_ = repo.CompleteOperation(context.WithoutCancel(ctx), op.ID, model.OperationStatusFailed, err)
			return resourceUnavailableError("container")
		}
		err = normalizeContainerRuntimeError(err)
		if s.requeueQueuedCreateIfRetryable(ctx, repo, op.ID, err) {
			return err
		}
		s.failQueuedLifecycle(ctx, repo, op.ID, containerID, msg.PreviousStatus, err)
		return err
	}

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
		OwnerID:              c.OwnerID.String(),
		Generation:           nextGeneration,
		ContainerName:        fmt.Sprintf("%s_g%d", strings.TrimPrefix(inspect.Name, "/"), nextGeneration),
		NetworkAlias:         networkAlias,
		ImageName:            inspect.Image,
		NetworkName:          userNetworkName(c.OwnerID),
		Domain:               fmt.Sprintf("%s.%s", msg.DomainPrefix, s.config.Get().BaseDomain),
		InternalPort:         msg.InternalPort,
		EnvVars:              append([]string(nil), inspect.Env...),
		MemoryLimitBytes:     inspect.MemoryLimitBytes,
		MemoryReservation:    inspect.MemoryReservation,
		MemorySwapMultiplier: s.config.Get().ContainerMemorySwapMultiplier,
		CPUShares:            inspect.CPUShares,
		CPUQuota:             inspect.CPUQuota,
		CPUPeriod:            inspect.CPUPeriod,
		PidsLimit:            s.config.Get().ContainerPidsLimit,
		ProxyNetworkName:     s.config.Get().ProxyNetworkName,
		VolumeMounts:         append([]model.ContainerMountSpec(nil), inspect.Mounts...),
		MaxLogSize:           s.config.Get().MaxLogSize,
		MaxLogFiles:          s.config.Get().MaxLogFiles,
		StorageQuota:         s.config.Get().ContainerDiskQuota,
		Command:              inspect.Command,
		Entrypoint:           inspect.Entrypoint,
		Restart:              inspect.Restart,
		Healthcheck:          inspect.Healthcheck,
	}
	if c.ProjectID != nil {
		dockerParams.ProjectID = c.ProjectID.String()
	}
	newDockerID, err := s.dockerAPI.CreateContainer(ctx, dockerParams)
	if err != nil {
		err = normalizeContainerRuntimeError(err)
		if s.requeueQueuedCreateIfRetryable(ctx, repo, op.ID, err) {
			return err
		}
		s.failQueuedLifecycle(ctx, repo, op.ID, containerID, msg.PreviousStatus, err)
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
	if msg.PreviousStatus == model.ContainerStatusRunning || c.DesiredStatus == model.ContainerStatusRunning {
		if err = s.dockerAPI.StartContainer(ctx, newDockerID); err != nil {
			err = normalizeContainerRuntimeError(err)
			if s.requeueQueuedCreateIfRetryable(ctx, repo, op.ID, err) {
				return err
			}
			s.failQueuedLifecycle(ctx, repo, op.ID, containerID, msg.PreviousStatus, err)
			return err
		}
		newStarted = true
	}
	if err = repo.UpdateDockerIDRoutingAndGeneration(ctx, containerID, newDockerID, msg.DomainPrefix, msg.InternalPort, nextGeneration); err != nil {
		if newStarted {
			cleanupCtx, cancel := detachedCleanupContext(ctx)
			_ = s.dockerAPI.StopContainer(cleanupCtx, newDockerID, s.config.Get().ContainerStopTimeout)
			cancel()
		}
		s.failQueuedLifecycle(ctx, repo, op.ID, containerID, msg.PreviousStatus, err)
		return err
	}
	targetStatus := msg.PreviousStatus
	if targetStatus == "" || targetStatus == model.ContainerStatusExposing {
		targetStatus = model.ContainerStatusCreated
	}
	if err = repo.UpdateStatus(ctx, containerID, targetStatus); err != nil {
		s.failQueuedLifecycle(ctx, repo, op.ID, containerID, msg.PreviousStatus, err)
		return err
	}
	if c.DockerID != "" {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		if removeErr := s.dockerAPI.RemoveContainer(cleanupCtx, c.DockerID, true); removeErr != nil && !isDockerNotFound(removeErr) {
			s.logger.WarnContext(cleanupCtx, "failed to remove old exposed container generation", "container_id", containerID, "docker_id", c.DockerID, "error", normalizeContainerRuntimeError(removeErr))
		}
		cancel()
	}
	_ = repo.CompleteOperation(context.WithoutCancel(ctx), op.ID, model.OperationStatusDone, nil)
	s.refreshProjectStatus(ctx, c.ProjectID)
	s.logger.InfoContext(ctx, "container exposed", "container_id", containerID, "owner_id", c.OwnerID, "domain_prefix", msg.DomainPrefix, "internal_port", msg.InternalPort)
	err = nil
	return nil
}

func (s *ContainerService) inspectQueuedCreateVolumes(ctx context.Context, mounts []model.VolumeMountParams) error {
	inspector, ok := s.dockerAPI.(containerVolumeInspector)
	if !ok {
		return nil
	}
	for _, mount := range mounts {
		if _, err := inspector.InspectVolume(ctx, mount.VolumeName); err != nil {
			if isDockerNotFound(err) {
				s.markVolumeMissing(ctx, mount.VolumeID)
				return resourceUnavailableError("volume")
			}
			return err
		}
	}
	return nil
}

func (s *ContainerService) resolveQueuedCreateImage(ctx context.Context, c model.Container) (string, *model.Image, bool, error) {
	baseName, version := parseImageTag(c.ImageTag)
	normalizedInputTag := fmt.Sprintf("%s:%s", baseName, version)
	isCustom := false
	var customImage *model.Image
	userImages, _, err := s.imageRepo.List(ctx, model.ListOptions{OwnerID: &c.OwnerID})
	if err == nil {
		for _, img := range userImages {
			if img.Tag == normalizedInputTag {
				isCustom = true
				customImage = &img
				break
			}
		}
	}
	if customImage != nil && customImage.Status == model.ImageStatusMissing {
		return "", nil, false, resourceUnavailableError("image")
	}
	if isCustom {
		return customImageFullTag(s.config.Get().RegistryPublicURL, c.OwnerID, baseName, version), customImage, true, nil
	}
	return normalizedInputTag, nil, false, nil
}

func (s *ContainerService) queuedCreateRuntimeSpec(c model.Container, mounts []model.VolumeMountParams, actualImageTag, networkName string) (model.ContainerRuntimeSpec, error) {
	cfg := s.config.Get()
	var envMap map[string]string
	if len(c.EnvVars) > 0 {
		if err := json.Unmarshal(c.EnvVars, &envMap); err != nil {
			return model.ContainerRuntimeSpec{}, err
		}
	}
	var envList []string
	for k, v := range envMap {
		envList = append(envList, fmt.Sprintf("%s=%s", k, v))
	}
	dockerMounts := make([]model.ContainerMountSpec, 0, len(mounts))
	for _, m := range mounts {
		dockerMounts = append(dockerMounts, model.ContainerMountSpec{
			VolumeName: m.VolumeName,
			Target:     m.MountPath,
			ReadOnly:   m.IsReadOnly,
		})
	}
	fullDomain := ""
	if c.DomainPrefix != "" {
		fullDomain = fmt.Sprintf("%s.%s", c.DomainPrefix, cfg.BaseDomain)
	}
	networkAlias := c.NetworkAlias
	if networkAlias == "" {
		networkAlias = c.Name
	}
	spec := model.ContainerRuntimeSpec{
		ContainerID:          c.ID.String(),
		OwnerID:              c.OwnerID.String(),
		Generation:           c.DockerGeneration,
		ContainerName:        fmt.Sprintf("usr_%s", c.ID.String()[:12]),
		NetworkAlias:         networkAlias,
		ImageName:            actualImageTag,
		NetworkName:          networkName,
		Domain:               fullDomain,
		InternalPort:         c.InternalPort,
		EnvVars:              envList,
		MemoryLimitBytes:     c.BaseMemoryReservation,
		MemoryReservation:    c.BaseMemoryReservation,
		MemorySwapMultiplier: cfg.ContainerMemorySwapMultiplier,
		CPUShares:            cfg.DefaultCPUShares,
		CPUQuota:             cpuQuotaFromMillicores(normalizeCPUReservation(c.BaseCPUReservation, cfg), cfg.ContainerCPUPeriod),
		CPUPeriod:            cfg.ContainerCPUPeriod,
		PidsLimit:            cfg.ContainerPidsLimit,
		ProxyNetworkName:     cfg.ProxyNetworkName,
		VolumeMounts:         dockerMounts,
		MaxLogSize:           cfg.MaxLogSize,
		MaxLogFiles:          cfg.MaxLogFiles,
		StorageQuota:         cfg.ContainerDiskQuota,
		Command:              c.Command,
		Entrypoint:           c.Entrypoint,
		Restart:              c.Restart,
		Healthcheck:          c.Healthcheck,
	}
	if c.ProjectID != nil {
		spec.ProjectID = c.ProjectID.String()
	}
	return spec, nil
}

func (s *ContainerService) ensureCreateOperationRunning(ctx context.Context, repo containerCreateWorkerRepository, operationID uuid.UUID) error {
	op, err := repo.GetOperationByID(ctx, operationID)
	if err != nil {
		return err
	}
	if op.Status != model.OperationStatusRunning {
		return apperrors.New(apperrors.ErrConflict, "container create operation is no longer active")
	}
	return nil
}

func (s *ContainerService) requeueQueuedCreateIfRetryable(ctx context.Context, repo containerCreateWorkerRepository, operationID uuid.UUID, cause error) bool {
	if !errors.Is(cause, apperrors.ErrTimeout) && !errors.Is(cause, apperrors.ErrUnavailable) {
		return false
	}
	if err := repo.RequeueOperation(context.WithoutCancel(ctx), operationID, cause); err != nil {
		s.logger.WarnContext(ctx, "failed to requeue container create operation", "operation_id", operationID, "error", err)
		return false
	}
	return true
}

func (s *ContainerService) failQueuedCreate(ctx context.Context, repo containerCreateWorkerRepository, operationID, containerID uuid.UUID, cause error, detached bool) {
	writeCtx := ctx
	var cancel context.CancelFunc
	if detached || ctx.Err() != nil {
		writeCtx, cancel = detachedContainerStateContext(ctx)
		defer cancel()
	}
	if err := repo.MarkStatusError(writeCtx, containerID, model.ContainerStatusError, normalizeContainerRuntimeError(cause)); err != nil {
		s.logger.WarnContext(writeCtx, "failed to mark queued container create error", "container_id", containerID, "error", err)
	}
	if err := repo.CompleteOperation(writeCtx, operationID, model.OperationStatusFailed, normalizeContainerRuntimeError(cause)); err != nil {
		s.logger.WarnContext(writeCtx, "failed to fail queued container create operation", "container_id", containerID, "operation_id", operationID, "error", err)
	}
}

func (s *ContainerService) failQueuedLifecycle(ctx context.Context, repo containerCreateWorkerRepository, operationID, containerID uuid.UUID, previousStatus string, cause error) {
	writeCtx, cancel := detachedContainerStateContext(ctx)
	defer cancel()
	restoreStatus := previousStatus
	switch restoreStatus {
	case "", model.ContainerStatusPending, model.ContainerStatusCreating, model.ContainerStatusStarting, model.ContainerStatusStopping, model.ContainerStatusExposing, model.ContainerStatusDeleting:
		restoreStatus = model.ContainerStatusError
	}
	if err := repo.MarkStatusError(writeCtx, containerID, restoreStatus, normalizeContainerRuntimeError(cause)); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		s.logger.WarnContext(writeCtx, "failed to restore failed container lifecycle status", "container_id", containerID, "status", restoreStatus, "error", err)
	}
	if err := repo.CompleteOperation(writeCtx, operationID, model.OperationStatusFailed, normalizeContainerRuntimeError(cause)); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		s.logger.WarnContext(writeCtx, "failed to fail queued container lifecycle operation", "container_id", containerID, "operation_id", operationID, "error", err)
	}
}
