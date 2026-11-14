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
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/stagedsource"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/validation"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
)

type VolumeRepository interface {
	volumeDiskUsageRepository
	volumeStateRepository
	Save(ctx context.Context, vol model.Volume) error
	// MarkAvailable records a volume Docker has created, with whether its
	// size is kernel-enforced. The flag is written only here and never changes
	// afterwards.
	MarkAvailable(ctx context.Context, id uuid.UUID, sizeEnforced bool) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Volume, error)
	GetByName(ctx context.Context, ownerID uuid.UUID, name string) (model.Volume, error)
	LinkProjectVolume(ctx context.Context, projectID, volumeID uuid.UUID) error
	UnlinkProjectVolume(ctx context.Context, projectID, volumeID uuid.UUID) error
	IsLinkedToProject(ctx context.Context, projectID, volumeID uuid.UUID) (bool, error)
	Delete(ctx context.Context, id uuid.UUID) error
	CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error)
	IsVolumeInUse(ctx context.Context, volumeID uuid.UUID) (bool, error)
	List(ctx context.Context, opts model.ListOptions) ([]model.Volume, int, error)
}

type volumeDiskUsageRepository interface {
	GetUserReservedVolumeBytes(ctx context.Context, ownerID uuid.UUID) (int64, error)
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

// VolumeCapacityLock serialises admission for one owner, so two creates
// cannot both count the same free disk quota.
type VolumeCapacityLock interface {
	AcquireOwnerCapacityLock(ctx context.Context, ownerID uuid.UUID) (func(), error)
}

// VolumeSizeModeReader reports the mode new volumes are created in.
type VolumeSizeModeReader interface {
	VolumeSizeMode() model.VolumeSizeMode
}

type VolumeDockerAPI interface {
	CreateVolume(ctx context.Context, params model.VolumeRuntimeSpec) (string, error)
	RemoveVolume(ctx context.Context, volumeName string, force bool) error
}

type VolumeService struct {
	repo         VolumeRepository
	dockerAPI    VolumeDockerAPI
	cfg          ConfigManager
	quotas       UserQuotaProvider
	imageRepo    volumeImageDiskRepository
	diskMetrics  HostDiskMetricsProvider
	hostDiskPath string
	lifecycle    VolumeLifecycleRepository
	capacityLock VolumeCapacityLock
	sizeMode     VolumeSizeModeReader
	logger       *slog.Logger
}

// VolumeServiceDeps contains the mandatory lifecycle and admission capabilities.
// An empty HostDiskPath uses the host filesystem root for disk admission.
// Logger is optional.
type VolumeServiceDeps struct {
	Repository   VolumeRepository
	Docker       VolumeDockerAPI
	Config       ConfigManager
	Lifecycle    VolumeLifecycleRepository
	Quotas       UserQuotaProvider
	ImageRepo    volumeImageDiskRepository
	DiskMetrics  HostDiskMetricsProvider
	CapacityLock VolumeCapacityLock
	SizeMode     VolumeSizeModeReader
	HostDiskPath string
	Logger       *slog.Logger
}

func NewVolumeService(deps VolumeServiceDeps) (*VolumeService, error) {
	required := []struct {
		name  string
		value any
	}{
		{"repository", deps.Repository}, {"docker", deps.Docker},
		{"config", deps.Config}, {"lifecycle", deps.Lifecycle},
		{"quotas", deps.Quotas}, {"images", deps.ImageRepo}, {"disk metrics", deps.DiskMetrics},
		{"capacity lock", deps.CapacityLock}, {"volume size mode", deps.SizeMode},
	}
	for _, dependency := range required {
		if dependency.value == nil || isNilServiceDependency(dependency.value) {
			return nil, fmt.Errorf("volume service: %s is required", dependency.name)
		}
	}
	return &VolumeService{
		repo:         deps.Repository,
		dockerAPI:    deps.Docker,
		cfg:          deps.Config,
		lifecycle:    deps.Lifecycle,
		quotas:       deps.Quotas,
		imageRepo:    deps.ImageRepo,
		diskMetrics:  deps.DiskMetrics,
		hostDiskPath: deps.HostDiskPath,
		capacityLock: deps.CapacityLock,
		sizeMode:     deps.SizeMode,
		logger:       logging.WithComponent(deps.Logger, "volume_service"),
	}, nil
}

// admitVolumeSize checks that sizeBytes fits what the owner's reserved disk
// leaves of their disk quota. The caller holds the owner capacity lock.
func (s *VolumeService) admitVolumeSize(ctx context.Context, ownerID uuid.UUID, sizeBytes int64) error {
	sources := admissionSources{
		quotas:  s.quotas,
		images:  s.imageRepo,
		volumes: s.repo,
		logger:  s.logger,
	}
	return sources.admit(ctx, ownerID, admissionRequest{userDiskBytes: sizeBytes}, s.cfg.Get(), "volume")
}

// volumeSize is the size a create asks for, or the default when it asks for
// none. A size below the minimum is a bad request.
func volumeSize(requested int64, cfg config.SystemConfig) (int64, error) {
	if requested == 0 {
		return cfg.DefaultVolumeSizeBytes, nil
	}
	if requested < config.MinVolumeSizeBytes {
		return 0, apperrors.New(apperrors.ErrBadRequest, "volume size must be at least 1 MiB")
	}
	return requested, nil
}

func (s *VolumeService) Create(ctx context.Context, params model.VolumeCreateParams) (uuid.UUID, error) {
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if err := validation.ResourceName(params.Name); err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	cfg := s.cfg.Get()
	sizeBytes, err := volumeSize(params.SizeBytes, cfg)
	if err != nil {
		return uuid.Nil, err
	}

	unlock, err := s.capacityLock.AcquireOwnerCapacityLock(ctx, ownerID)
	if err != nil {
		return uuid.Nil, err
	}
	defer unlock()
	if err := s.admitVolumeSize(ctx, ownerID, sizeBytes); err != nil {
		return uuid.Nil, err
	}
	if err := stagedsource.CheckHostDiskFloor(s.diskMetrics, s.hostDiskPath, s.cfg.Get().HostMinFreeDiskBytes, 0); err != nil {
		return uuid.Nil, err
	}

	count, err := s.repo.CountByOwnerID(ctx, ownerID)
	if err != nil {
		return uuid.Nil, err
	}
	if count >= cfg.MaxVolumesPerUser {
		return uuid.Nil, apperrors.ErrLimitExceeded
	}

	volID := uuid.New()
	// The Docker name is unique on the host, so it comes from the volume ID,
	// never from owner or user-facing name.
	dockerName := "vol_" + volID.String()

	vol := model.Volume{
		ID:         volID,
		OwnerID:    ownerID,
		ProjectID:  params.ProjectID,
		Name:       params.Name,
		DockerName: dockerName,
		Status:     model.VolumeStatusCreating,
		SizeBytes:  sizeBytes,
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

func (s *VolumeService) ResolveProjectManagedByName(ctx context.Context, projectID uuid.UUID, name string) (uuid.UUID, bool, error) {
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		return uuid.Nil, false, err
	}
	if err := validation.ResourceName(name); err != nil {
		return uuid.Nil, false, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}

	vol, err := s.repo.GetByName(ctx, ownerID, name)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			id, createErr := s.Create(ctx, model.VolumeCreateParams{
				ProjectID: &projectID,
				Name:      name,
			})
			return id, false, createErr
		}
		return uuid.Nil, false, err
	}
	switch vol.Status {
	case model.VolumeStatusAvailable:
		if err := s.repo.LinkProjectVolume(ctx, projectID, vol.ID); err != nil {
			return uuid.Nil, false, err
		}
		return vol.ID, true, nil
	case model.VolumeStatusCreating:
		linked, err := s.repo.IsLinkedToProject(ctx, projectID, vol.ID)
		if err != nil {
			return uuid.Nil, false, err
		}
		if linked {
			return vol.ID, false, nil
		}
		return uuid.Nil, false, apperrors.New(apperrors.ErrConflict, "volume is not available")
	case model.VolumeStatusMissing:
		return uuid.Nil, false, resourceUnavailableError("volume")
	case model.VolumeStatusDeleting, model.VolumeStatusError:
		return uuid.Nil, false, apperrors.New(apperrors.ErrConflict, "volume is not available")
	default:
		return uuid.Nil, false, apperrors.New(apperrors.ErrConflict, "volume is not available")
	}
}

func (s *VolumeService) UnlinkProjectVolume(ctx context.Context, projectID, volumeID uuid.UUID) error {
	vol, err := s.repo.GetByID(ctx, volumeID)
	if err != nil {
		return err
	}
	if err := accessscope.RequireOwnerAccess(ctx, vol.OwnerID); err != nil {
		return err
	}
	return s.repo.UnlinkProjectVolume(ctx, projectID, volumeID)
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
	op, claimed, err := s.lifecycle.ClaimPendingOperation(ctx, operationID, s.cfg.Get().ContainerCreateMaxAttempts)
	if err != nil {
		return err
	}
	if !claimed {
		if op.Status == model.OperationStatusPending && s.cfg.Get().ContainerCreateMaxAttempts > 0 && op.Attempts >= s.cfg.Get().ContainerCreateMaxAttempts {
			cause := apperrors.New(apperrors.ErrTimeout, "volume lifecycle attempts exhausted")
			return s.failQueuedVolume(ctx, operationID, volumeID, cause)
		}
		return nil
	}
	if op.ResourceType != model.ResourceTypeVolume || op.ResourceID != volumeID {
		cause := apperrors.New(apperrors.ErrBadRequest, "volume lifecycle message does not match operation")
		return errors.Join(cause, s.completeVolumeOperation(ctx, operationID, model.OperationStatusFailed, cause))
	}
	switch op.Operation {
	case model.OperationCreate:
		return s.executeQueuedVolumeCreate(ctx, operationID, volumeID)
	case model.OperationDelete:
		return s.executeQueuedVolumeDelete(ctx, operationID, volumeID)
	default:
		cause := apperrors.New(apperrors.ErrBadRequest, "unsupported volume lifecycle operation")
		return errors.Join(cause, s.failQueuedVolume(ctx, operationID, volumeID, cause))
	}
}

func (s *VolumeService) executeQueuedVolumeCreate(ctx context.Context, operationID, volumeID uuid.UUID) error {
	vol, err := s.repo.GetByID(ctx, volumeID)
	if err != nil {
		return errors.Join(err, s.completeVolumeOperation(ctx, operationID, model.OperationStatusFailed, err))
	}
	dockerParams := model.VolumeRuntimeSpec{
		VolumeName: vol.DockerName,
		VolumeID:   vol.ID.String(),
		Name:       vol.Name,
		OwnerID:    vol.OwnerID.String(),
	}
	if vol.ProjectID != nil {
		dockerParams.ProjectID = vol.ProjectID.String()
	}
	// The one place a volume's mode is decided. Docker refusing the size
	// fails this create like any other failure, without falling back.
	sizeEnforced := s.sizeMode.VolumeSizeMode() == model.VolumeSizeEnforced
	if sizeEnforced {
		dockerParams.SizeBytes = vol.SizeBytes
	}
	if _, err := s.dockerAPI.CreateVolume(ctx, dockerParams); err != nil {
		return s.retryOrFailQueuedVolume(ctx, operationID, volumeID, normalizeContainerRuntimeError(err))
	}
	if err := s.repo.MarkAvailable(ctx, volumeID, sizeEnforced); err != nil {
		// Docker volume creation is idempotent by name, so a retried operation
		// can record the status the failed write left behind.
		err = fmt.Errorf("record volume %s status: %w", model.VolumeStatusAvailable, err)
		return s.retryOrFailQueuedVolume(ctx, operationID, volumeID, err)
	}
	return s.completeVolumeOperation(ctx, operationID, model.OperationStatusDone, nil)
}

func (s *VolumeService) executeQueuedVolumeDelete(ctx context.Context, operationID, volumeID uuid.UUID) error {
	vol, err := s.repo.GetByID(ctx, volumeID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return s.completeVolumeOperation(ctx, operationID, model.OperationStatusDone, nil)
		}
		return errors.Join(err, s.completeVolumeOperation(ctx, operationID, model.OperationStatusFailed, err))
	}
	if err := s.dockerAPI.RemoveVolume(ctx, vol.DockerName, false); err != nil && !cerrdefs.IsNotFound(err) {
		return s.retryOrFailQueuedVolume(ctx, operationID, volumeID, normalizeContainerRuntimeError(err))
	}
	if err := s.repo.Delete(ctx, volumeID); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return errors.Join(err, s.failQueuedVolume(ctx, operationID, volumeID, err))
	}
	return s.completeVolumeOperation(ctx, operationID, model.OperationStatusDone, nil)
}

// completeVolumeOperation records the operation's terminal status. A failed
// write leaves the operation running until restart recovery requeues it, so
// the error is returned for the resource queue consumer to log with the
// operation ID.
func (s *VolumeService) completeVolumeOperation(ctx context.Context, operationID uuid.UUID, status string, cause error) error {
	if err := s.lifecycle.CompleteOperation(context.WithoutCancel(ctx), operationID, status, cause); err != nil {
		return fmt.Errorf("record volume operation %s status: %w", status, err)
	}
	return nil
}

// retryOrFailQueuedVolume returns a retryable failure's operation to pending,
// or otherwise marks the volume and its operation failed. It returns cause
// joined with any failed requeue or status write.
func (s *VolumeService) retryOrFailQueuedVolume(ctx context.Context, operationID, volumeID uuid.UUID, cause error) error {
	if !retryableLifecycleError(cause) {
		return errors.Join(cause, s.failQueuedVolume(ctx, operationID, volumeID, cause))
	}
	requeueErr := s.lifecycle.RequeueOperation(context.WithoutCancel(ctx), operationID, cause)
	if requeueErr == nil {
		return cause
	}
	requeueErr = fmt.Errorf("requeue volume operation: %w", requeueErr)
	return errors.Join(cause, requeueErr, s.failQueuedVolume(ctx, operationID, volumeID, cause))
}

// failQueuedVolume marks the volume and its operation failed and returns any
// failed status write.
func (s *VolumeService) failQueuedVolume(ctx context.Context, operationID, volumeID uuid.UUID, cause error) error {
	writeCtx, cancel := detachedContainerStateContext(ctx)
	defer cancel()
	cause = normalizeContainerRuntimeError(cause)
	var markErr error
	if err := s.repo.MarkStatusError(writeCtx, volumeID, model.VolumeStatusError, cause); err != nil {
		markErr = fmt.Errorf("record volume %s status: %w", model.VolumeStatusError, err)
	}
	return errors.Join(markErr, s.completeVolumeOperation(writeCtx, operationID, model.OperationStatusFailed, cause))
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
	msg := queue.ResourceLifecycleMessage{
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
		Exchange:     queue.ResourceExchangeName,
		RoutingKey:   queue.ResourceRoutingKey,
		Payload:      payload,
		Status:       model.ResourceOutboxStatusPending,
	}, nil
}
