package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/callmerussell04/docker-cloud-manager/pkg/resourcequeue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
)

type VolumeRepository interface {
	Save(ctx context.Context, vol model.Volume) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Volume, error)
	GetByName(ctx context.Context, ownerID uuid.UUID, name string) (model.Volume, error)
	Delete(ctx context.Context, id uuid.UUID) error
	CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error)
	IsVolumeInUse(ctx context.Context, volumeID uuid.UUID) (bool, error)
	List(ctx context.Context, opts model.ListOptions) ([]model.Volume, int, error)
}

type volumeDiskUsageRepository interface {
	GetUserUsedVolumeBytes(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

type volumeImageDiskRepository interface {
	GetUserUsedDiskSpace(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

type volumeStateRepository interface {
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error
}

type VolumeLifecycleRepository interface {
	CreateQueuedVolume(ctx context.Context, vol model.Volume, op model.ResourceOperation, outbox model.ResourceLifecycleOutbox) error
	QueueVolumeDelete(ctx context.Context, id uuid.UUID, op model.ResourceOperation, outbox model.ResourceLifecycleOutbox) error
	HasActiveOperation(ctx context.Context, resourceType string, resourceID uuid.UUID) (bool, error)
	GetOperationByID(ctx context.Context, id uuid.UUID) (model.ResourceOperation, error)
	ClaimPendingOperation(ctx context.Context, id uuid.UUID, maxAttempts int) (model.ResourceOperation, bool, error)
	CompleteOperation(ctx context.Context, id uuid.UUID, status string, cause error) error
	RequeueOperation(ctx context.Context, id uuid.UUID, cause error) error
}

type VolumeDockerAPI interface {
	CreateVolume(ctx context.Context, params model.VolumeRuntimeSpec) (string, error)
	RemoveVolume(ctx context.Context, volumeName string, force bool) error
}

type VolumeService struct {
	repo         VolumeRepository
	dockerAPI    VolumeDockerAPI
	cfg          ConfigManager
	users        UserInfoProvider
	imageRepo    volumeImageDiskRepository
	diskMetrics  HostDiskMetricsProvider
	hostDiskPath string
	lifecycle    VolumeLifecycleRepository
}

type VolumeServiceDeps struct {
	Users        UserInfoProvider
	ImageRepo    volumeImageDiskRepository
	DiskMetrics  HostDiskMetricsProvider
	HostDiskPath string
}

func (s *VolumeService) SetLifecycleRepository(repo VolumeLifecycleRepository) {
	s.lifecycle = repo
}

func NewVolumeService(repo VolumeRepository, dockerAPI VolumeDockerAPI, cfg ConfigManager, deps VolumeServiceDeps) *VolumeService {
	return &VolumeService{
		repo:         repo,
		dockerAPI:    dockerAPI,
		cfg:          cfg,
		users:        deps.Users,
		imageRepo:    deps.ImageRepo,
		diskMetrics:  deps.DiskMetrics,
		hostDiskPath: deps.HostDiskPath,
	}
}

func (s *VolumeService) ensureDiskQuotaAvailable(ctx context.Context, ownerID uuid.UUID) error {
	volumeRepo, _ := s.repo.(volumeDiskUsageRepository)
	return ensureDiskQuotaAvailable(ctx, ownerID, s.users, s.imageRepo, volumeRepo)
}

func (s *VolumeService) ensureHostDiskFloor() error {
	if s.cfg == nil {
		return nil
	}
	return ensureHostDiskFloor(s.diskMetrics, s.hostDiskPath, s.cfg.Get().HostMinFreeDiskBytes)
}

func (s *VolumeService) Create(ctx context.Context, params model.VolumeCreateParams) (uuid.UUID, error) {
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if err := validation.ResourceName(params.Name); err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	if err := s.ensureDiskQuotaAvailable(ctx, ownerID); err != nil {
		return uuid.Nil, err
	}
	if err := s.ensureHostDiskFloor(); err != nil {
		return uuid.Nil, err
	}

	// Проверка лимита на количество томов
	count, err := s.repo.CountByOwnerID(ctx, ownerID)
	if err != nil {
		return uuid.Nil, err
	}
	if count >= s.cfg.Get().MaxVolumesPerUser {
		return uuid.Nil, apperrors.ErrLimitExceeded
	}

	volID := uuid.New()
	dockerName := fmt.Sprintf("vol_%s_%s", ownerID.String()[:8], params.Name)

	vol := model.Volume{
		ID:         volID,
		OwnerID:    ownerID,
		ProjectID:  params.ProjectID,
		Name:       params.Name,
		DockerName: dockerName,
		Status:     model.VolumeStatusCreating,
	}

	if s.lifecycle == nil {
		return uuid.Nil, apperrors.New(apperrors.ErrUnavailable, "volume lifecycle queue is unavailable")
	}
	op, outbox, err := resourceOperationOutbox(ctx, model.ResourceTypeVolume, volID, ownerID, model.OperationCreate)
	if err != nil {
		return uuid.Nil, err
	}
	if err := s.lifecycle.CreateQueuedVolume(ctx, vol, op, outbox); err != nil {
		return uuid.Nil, err
	}
	return volID, nil
}

func (s *VolumeService) ResolveByName(ctx context.Context, name string) (uuid.UUID, error) {
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if err := validation.ResourceName(name); err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}

	vol, err := s.repo.GetByName(ctx, ownerID, name)
	if err != nil {
		return uuid.Nil, err
	}
	switch vol.Status {
	case model.VolumeStatusAvailable:
		return vol.ID, nil
	case model.VolumeStatusMissing:
		return uuid.Nil, resourceUnavailableError("volume")
	case model.VolumeStatusDeleting, model.VolumeStatusError, model.VolumeStatusCreating:
		return uuid.Nil, apperrors.New(apperrors.ErrConflict, "volume is not available")
	default:
		return uuid.Nil, apperrors.New(apperrors.ErrConflict, "volume is not available")
	}
}

func (s *VolumeService) Delete(ctx context.Context, volumeID uuid.UUID) error {
	vol, err := s.repo.GetByID(ctx, volumeID)
	if err != nil {
		return err
	}

	if err := accessscope.RequireOwnerAccess(ctx, vol.OwnerID); err != nil {
		return err
	}

	inUse, err := s.repo.IsVolumeInUse(ctx, volumeID)
	if err != nil {
		return err
	}
	if inUse {
		return apperrors.New(apperrors.ErrResourceInUse, "volume is currently used by a container")
	}
	if s.lifecycle == nil {
		return apperrors.New(apperrors.ErrUnavailable, "volume lifecycle queue is unavailable")
	}
	active, err := s.lifecycle.HasActiveOperation(ctx, model.ResourceTypeVolume, volumeID)
	if err != nil {
		return err
	}
	if active {
		return apperrors.New(apperrors.ErrConflict, "resource operation is already in progress")
	}
	op, outbox, err := resourceOperationOutbox(ctx, model.ResourceTypeVolume, volumeID, vol.OwnerID, model.OperationDelete)
	if err != nil {
		return err
	}
	return s.lifecycle.QueueVolumeDelete(ctx, volumeID, op, outbox)
}

func (s *VolumeService) List(ctx context.Context, limit, offset int) ([]model.Volume, int, error) {
	scope, err := accessscope.RequireScope(ctx)
	if err != nil {
		return nil, 0, err
	}
	if scope.Kind != accessscope.KindUser && scope.Kind != accessscope.KindAdmin {
		return nil, 0, apperrors.ErrForbidden
	}
	return s.repo.List(ctx, model.ListOptions{
		OwnerID: scope.OwnerFilter(),
		Limit:   limit,
		Offset:  offset,
	})
}

func (s *VolumeService) GetByID(ctx context.Context, id uuid.UUID) (model.Volume, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *VolumeService) ExecuteQueuedVolumeOperation(ctx context.Context, operationID, volumeID uuid.UUID) error {
	if s.lifecycle == nil {
		return apperrors.New(apperrors.ErrUnavailable, "volume lifecycle queue is unavailable")
	}
	op, claimed, err := s.lifecycle.ClaimPendingOperation(ctx, operationID, s.cfg.Get().ContainerCreateMaxAttempts)
	if err != nil {
		return err
	}
	if !claimed {
		if op.Status == model.OperationStatusPending && s.cfg.Get().ContainerCreateMaxAttempts > 0 && op.Attempts >= s.cfg.Get().ContainerCreateMaxAttempts {
			cause := apperrors.New(apperrors.ErrTimeout, "volume lifecycle attempts exhausted")
			s.failQueuedVolume(ctx, operationID, volumeID, model.VolumeStatusError, cause)
		}
		return nil
	}
	if op.ResourceType != model.ResourceTypeVolume || op.ResourceID != volumeID {
		cause := apperrors.New(apperrors.ErrBadRequest, "volume lifecycle message does not match operation")
		_ = s.lifecycle.CompleteOperation(context.WithoutCancel(ctx), operationID, model.OperationStatusFailed, cause)
		return cause
	}
	switch op.Operation {
	case model.OperationCreate:
		return s.executeQueuedVolumeCreate(ctx, operationID, volumeID)
	case model.OperationDelete:
		return s.executeQueuedVolumeDelete(ctx, operationID, volumeID)
	default:
		cause := apperrors.New(apperrors.ErrBadRequest, "unsupported volume lifecycle operation")
		s.failQueuedVolume(ctx, operationID, volumeID, model.VolumeStatusError, cause)
		return cause
	}
}

func (s *VolumeService) executeQueuedVolumeCreate(ctx context.Context, operationID, volumeID uuid.UUID) error {
	vol, err := s.repo.GetByID(ctx, volumeID)
	if err != nil {
		_ = s.lifecycle.CompleteOperation(context.WithoutCancel(ctx), operationID, model.OperationStatusFailed, err)
		return err
	}
	dockerParams := model.VolumeRuntimeSpec{
		VolumeName: vol.DockerName,
		VolumeID:   vol.ID.String(),
		OwnerID:    vol.OwnerID.String(),
	}
	if vol.ProjectID != nil {
		dockerParams.ProjectID = vol.ProjectID.String()
	}
	if _, err := s.dockerAPI.CreateVolume(ctx, dockerParams); err != nil {
		err = normalizeContainerRuntimeError(err)
		if s.requeueQueuedVolumeIfRetryable(ctx, operationID, err) {
			return err
		}
		s.failQueuedVolume(ctx, operationID, volumeID, model.VolumeStatusError, err)
		return err
	}
	s.setVolumeStatus(ctx, volumeID, model.VolumeStatusAvailable)
	_ = s.lifecycle.CompleteOperation(context.WithoutCancel(ctx), operationID, model.OperationStatusDone, nil)
	return nil
}

func (s *VolumeService) executeQueuedVolumeDelete(ctx context.Context, operationID, volumeID uuid.UUID) error {
	vol, err := s.repo.GetByID(ctx, volumeID)
	if err != nil {
		if err == apperrors.ErrNotFound {
			_ = s.lifecycle.CompleteOperation(context.WithoutCancel(ctx), operationID, model.OperationStatusDone, nil)
			return nil
		}
		_ = s.lifecycle.CompleteOperation(context.WithoutCancel(ctx), operationID, model.OperationStatusFailed, err)
		return err
	}
	if err := s.dockerAPI.RemoveVolume(ctx, vol.DockerName, false); err != nil && !cerrdefs.IsNotFound(err) {
		err = normalizeContainerRuntimeError(err)
		if s.requeueQueuedVolumeIfRetryable(ctx, operationID, err) {
			return err
		}
		s.failQueuedVolume(ctx, operationID, volumeID, model.VolumeStatusError, err)
		return err
	}
	if err := s.repo.Delete(ctx, volumeID); err != nil && err != apperrors.ErrNotFound {
		s.failQueuedVolume(ctx, operationID, volumeID, model.VolumeStatusError, err)
		return err
	}
	_ = s.lifecycle.CompleteOperation(context.WithoutCancel(ctx), operationID, model.OperationStatusDone, nil)
	return nil
}

func (s *VolumeService) requeueQueuedVolumeIfRetryable(ctx context.Context, operationID uuid.UUID, cause error) bool {
	if !retryableLifecycleError(cause) {
		return false
	}
	return s.lifecycle.RequeueOperation(context.WithoutCancel(ctx), operationID, cause) == nil
}

func (s *VolumeService) failQueuedVolume(ctx context.Context, operationID, volumeID uuid.UUID, status string, cause error) {
	writeCtx, cancel := detachedContainerStateContext(ctx)
	defer cancel()
	s.markVolumeError(writeCtx, volumeID, status, normalizeContainerRuntimeError(cause))
	_ = s.lifecycle.CompleteOperation(writeCtx, operationID, model.OperationStatusFailed, normalizeContainerRuntimeError(cause))
}

func (s *VolumeService) setVolumeStatus(ctx context.Context, volumeID uuid.UUID, status string) {
	stateRepo, ok := s.repo.(volumeStateRepository)
	if !ok {
		return
	}
	_ = stateRepo.UpdateStatus(ctx, volumeID, status)
}

func (s *VolumeService) markVolumeError(ctx context.Context, volumeID uuid.UUID, status string, cause error) {
	stateRepo, ok := s.repo.(volumeStateRepository)
	if !ok {
		return
	}
	_ = stateRepo.MarkStatusError(ctx, volumeID, status, cause)
}

func resourceOperationOutbox(ctx context.Context, resourceType string, resourceID, ownerID uuid.UUID, operation string) (model.ResourceOperation, model.ResourceLifecycleOutbox, error) {
	op := model.ResourceOperation{
		ID:           uuid.New(),
		ResourceType: resourceType,
		ResourceID:   resourceID,
		OwnerID:      ownerID,
		Operation:    operation,
		Status:       model.OperationStatusPending,
	}
	msg := resourcequeue.LifecycleMessage{
		OperationID:  op.ID.String(),
		ResourceID:   resourceID.String(),
		ResourceType: resourceType,
		OwnerID:      ownerID.String(),
		Operation:    operation,
		RequestID:    logging.RequestIDFromContext(ctx),
		CreatedAt:    time.Now().Unix(),
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return model.ResourceOperation{}, model.ResourceLifecycleOutbox{}, fmt.Errorf("failed to marshal resource lifecycle message: %w", err)
	}
	return op, model.ResourceLifecycleOutbox{
		ID:           uuid.New(),
		OperationID:  op.ID,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Exchange:     resourcequeue.ExchangeName,
		RoutingKey:   resourcequeue.RoutingKey,
		Payload:      payload,
		Status:       model.ResourceOutboxStatusPending,
	}, nil
}
