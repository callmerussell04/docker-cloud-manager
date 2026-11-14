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
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/cleanupctx"
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

// ExecuteQueuedContainerOperation runs one queued container operation through
// the lifecycle executor.
func (s *ContainerService) ExecuteQueuedContainerOperation(ctx context.Context, operationID, containerID uuid.UUID, msg queue.ContainerLifecycleMessage) error {
	op, err := s.repo.GetOperationByID(ctx, operationID)
	if err != nil {
		return err
	}
	return s.executeQueuedLifecycle(ctx, op, containerID, msg)
}

// runQueuedCreate is create's lifecycle step: it checks the volumes, ensures
// the user network, resolves and pulls the image and creates the Docker
// container. A container already recorded, as when a restart interrupted the
// create after its record write, needs no Docker work. Volume and image
// resolution failures are terminal; Docker timeouts and unavailability are
// retried.
func (s *ContainerService) runQueuedCreate(ctx context.Context, operationID uuid.UUID, c model.Container) (lifecycleChange, error) {
	var change lifecycleChange
	if c.DockerID != "" {
		return change, nil
	}
	mounts, err := s.repo.GetMountsByContainerID(ctx, c.ID)
	if err != nil {
		return change, terminal(err)
	}
	if err := s.ensureOperationRunning(ctx, operationID); err != nil {
		return change, err
	}
	if err := s.inspectQueuedCreateVolumes(ctx, mounts); err != nil {
		return change, terminal(err)
	}

	networkName := userNetworkName(c.OwnerID)
	if _, err := s.dockerAPI.EnsureUserNetwork(ctx, networkName); err != nil {
		return change, normalizeContainerRuntimeError(err)
	}
	change.discard = func(ctx context.Context) { s.cleanupUserNetworkIfUnused(ctx, c.OwnerID) }

	imageTag, customImage, isCustom, err := s.resolveContainerCreateImage(ctx, c)
	if err != nil {
		return change, terminal(err)
	}
	if err := s.ensureOperationRunning(ctx, operationID); err != nil {
		return change, err
	}
	pulled, err := s.ensureQueuedCreateImage(ctx, imageTag, customImage, isCustom)
	if err != nil {
		return change, err
	}
	if pulled {
		releaseNetwork := change.discard
		change.discard = func(ctx context.Context) {
			cleanupCtx, cancel := cleanupctx.Detached(ctx)
			_ = s.dockerAPI.RemoveImage(cleanupCtx, imageTag, false) // best-effort removal of an image pulled for a failed create; create's outcome stands
			cancel()
			releaseNetwork(ctx)
		}
	}

	if err := s.ensureOperationRunning(ctx, operationID); err != nil {
		return change, err
	}
	spec, err := s.queuedCreateRuntimeSpec(c, mounts, imageTag, networkName)
	if err != nil {
		return change, terminal(err)
	}
	dockerID, err := s.dockerAPI.CreateContainer(ctx, spec)
	if err != nil {
		return change, normalizeContainerRuntimeError(err)
	}
	change.created = dockerID
	change.record = func(ctx context.Context) error {
		return s.repo.UpdateDockerIDAndStatus(ctx, c.ID, dockerID, model.ContainerStatusCreated)
	}
	change.settled = func(ctx context.Context) {
		s.logger.InfoContext(ctx, "container created", "container_id", c.ID, "owner_id", c.OwnerID, "image_tag", c.ImageTag)
	}
	return change, nil
}

// ensureQueuedCreateImage makes imageTag available to Docker: a custom image
// is always pulled from the Registry, a public one only when Docker lacks it.
// It reports whether it pulled a public image, which a failed create removes.
func (s *ContainerService) ensureQueuedCreateImage(ctx context.Context, imageTag string, customImage *model.Image, isCustom bool) (bool, error) {
	if isCustom {
		if err := s.dockerAPI.PullImage(ctx, imageTag); err != nil {
			if customImage != nil && isDockerNotFound(err) {
				s.markImageMissing(ctx, customImage.ID)
			}
			return false, pullImageError(err)
		}
		return false, nil
	}
	exists, err := s.dockerAPI.ImageExists(ctx, imageTag)
	if err != nil {
		return false, normalizeContainerRuntimeError(err)
	}
	if exists {
		return false, nil
	}
	if err := s.checkHostDiskCapacity(); err != nil {
		return false, terminal(err)
	}
	if err := s.dockerAPI.PullImage(ctx, imageTag); err != nil {
		return false, pullImageError(err)
	}
	return true, nil
}

// pullImageError reports a failed pull as a bad request unless it timed out.
func pullImageError(err error) error {
	err = normalizeContainerRuntimeError(err)
	if errors.Is(err, apperrors.ErrTimeout) {
		return err
	}
	return apperrors.Wrap(apperrors.ErrBadRequest, "image could not be pulled", err)
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
		return customImageFullTag(s.config.Get().RegistryPublicURL, c.OwnerID, baseName, img.RegistryTag), &img, true, nil
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
	resources := containerResources(c, cfg)
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
		MemoryLimitBytes:     resources.MemoryLimitBytes,
		MemoryReservation:    resources.MemoryReservation,
		MemorySwapMultiplier: resources.MemorySwapMultiplier,
		CPUShares:            resources.CPUShares,
		CPUQuota:             resources.CPUQuota,
		CPUPeriod:            resources.CPUPeriod,
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

// ensureOperationRunning reports errLifecycleOperationInactive once the
// operation is no longer running, as when a delete canceled the create. Both
// that and a failed status read are fence errors, which the executor does not
// settle.
func (s *ContainerService) ensureOperationRunning(ctx context.Context, operationID uuid.UUID) error {
	op, err := s.repo.GetOperationByID(ctx, operationID)
	if err != nil {
		return fenceError{err: err}
	}
	if op.Status != model.OperationStatusRunning {
		return fenceError{err: errLifecycleOperationInactive}
	}
	return nil
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
