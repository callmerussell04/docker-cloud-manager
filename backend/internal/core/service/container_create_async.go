package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/validation"
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
	UpdateStatusAndTTLDeadline(ctx context.Context, id uuid.UUID, status string, ttlDeadline *time.Time) error
	UpdateDockerIDAndStatus(ctx context.Context, id uuid.UUID, dockerID string, status string) error
	UpdateDockerIDRoutingAndGeneration(ctx context.Context, id uuid.UUID, dockerID string, domainPrefix string, internalPort int, generation int) error
	MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error
	GetMountsByContainerID(ctx context.Context, containerID uuid.UUID) ([]model.VolumeMountParams, error)
}

type ContainerLifecycleConsumer interface {
	Run(ctx context.Context, workers int, handler func(context.Context, queue.ContainerLifecycleMessage) error)
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

func (w *ContainerCreateWorker) HandleMessage(ctx context.Context, msg queue.ContainerLifecycleMessage) error {
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

func (s *ContainerService) ExecuteQueuedContainerOperation(ctx context.Context, operationID, containerID uuid.UUID, msg queue.ContainerLifecycleMessage) error {
	repo := s.repo
	op, err := repo.GetOperationByID(ctx, operationID)
	if err != nil {
		return err
	}
	if op.Operation == model.OperationCreate {
		return s.ExecuteQueuedCreate(ctx, operationID, containerID)
	}
	return s.executeQueuedLifecycle(ctx, op, containerID, msg)
}

func (s *ContainerService) ExecuteQueuedCreate(ctx context.Context, operationID, containerID uuid.UUID) (err error) {
	repo := s.repo
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
		return errors.Join(cause, s.completeQueuedOperation(ctx, repo, operationID, model.OperationStatusFailed, cause))
	}

	if err := repo.UpdateStatus(ctx, containerID, model.ContainerStatusCreating); err != nil {
		if requeueErr := repo.RequeueOperation(context.WithoutCancel(ctx), operationID, err); requeueErr != nil {
			return errors.Join(err, fmt.Errorf("requeue container create operation: %w", requeueErr))
		}
		return err
	}

	c, err := repo.GetByID(ctx, containerID)
	if err != nil {
		return errors.Join(err, s.completeQueuedOperation(ctx, repo, operationID, model.OperationStatusFailed, err))
	}
	if c.DockerID != "" {
		return s.completeQueuedOperation(ctx, repo, operationID, model.OperationStatusDone, nil)
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

	actualImageTag, customImage, isCustom, err := s.resolveContainerCreateImage(ctx, c)
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
			_ = s.dockerAPI.RemoveImage(cleanupCtx, actualImageTag, false) // best-effort removal of an image pulled for a failed create; create's error is returned
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
		_ = s.dockerAPI.RemoveContainer(cleanupCtx, dockerID, true) // best-effort removal of a container DCM failed to record; the record error is returned
		cancel()
		s.failQueuedCreate(ctx, repo, operationID, containerID, err, true)
		return err
	}
	// Returning here would fire the deferred cleanups of the recorded
	// container's network and image, so a failed completion write is logged.
	if err := repo.CompleteOperation(ctx, operationID, model.OperationStatusDone, nil); err != nil {
		s.logger.WarnContext(ctx, "failed to complete container create operation", "container_id", containerID, "operation_id", operationID, "error", err)
	}
	s.logger.InfoContext(ctx, "container created", "container_id", containerID, "owner_id", c.OwnerID, "image_tag", c.ImageTag)
	return nil
}

func (s *ContainerService) inspectQueuedCreateVolumes(ctx context.Context, mounts []model.VolumeMountParams) error {
	inspector := s.dockerAPI
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

func (s *ContainerService) resolveContainerCreateImage(ctx context.Context, c model.Container) (string, *model.Image, bool, error) {
	if err := validation.ImageTag(c.ImageTag); err != nil {
		return "", nil, false, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	baseName, version := parseImageTag(c.ImageTag)
	normalizedInputTag := fmt.Sprintf("%s:%s", baseName, version)
	userImages, _, err := s.imageRepo.List(ctx, model.ListOptions{OwnerID: &c.OwnerID})
	if err != nil {
		return "", nil, false, fmt.Errorf("resolve owned container image: %w", err)
	}
	for _, img := range userImages {
		if img.Tag != normalizedInputTag {
			continue
		}
		if img.Status != model.ImageStatusAvailable {
			return "", nil, false, resourceUnavailableError("image")
		}
		return customImageFullTag(s.config.Get().RegistryPublicURL, c.OwnerID, baseName, version), &img, true, nil
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

// completeQueuedOperation records the operation's terminal status. A failed
// write leaves the operation running until restart recovery settles it, so the
// error is returned for the container queue consumer to log with the operation
// ID.
func (s *ContainerService) completeQueuedOperation(ctx context.Context, repo containerCreateWorkerRepository, operationID uuid.UUID, status string, cause error) error {
	if err := repo.CompleteOperation(context.WithoutCancel(ctx), operationID, status, cause); err != nil {
		return fmt.Errorf("record container operation %s status: %w", status, err)
	}
	return nil
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
