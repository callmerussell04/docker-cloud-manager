package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/containerqueue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/google/uuid"
)

type VolumeUsageRepo interface {
	GetReconcileCandidates(ctx context.Context) ([]model.Volume, error)
	UpdateUsage(ctx context.Context, id uuid.UUID, usedBytes int64) error
	GetOwnersWithVolumes(ctx context.Context) ([]uuid.UUID, error)
	GetUserUsedVolumeBytes(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

type VolumeUsageDockerAPI interface {
	GetVolumeUsageBytes(ctx context.Context, volumeName string) (int64, error)
}

type VolumeUsageContainerRepo interface {
	GetRunningWithWritableVolumeMounts(ctx context.Context, ownerID uuid.UUID) ([]model.Container, error)
	HasActiveOperation(ctx context.Context, resourceType string, resourceID uuid.UUID) (bool, error)
	QueueContainerOperation(ctx context.Context, id uuid.UUID, status string, desiredStatus string, op model.ResourceOperation, outbox model.ContainerLifecycleOutbox) error
}

type VolumeUsageImageRepo interface {
	GetUserUsedDiskSpace(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

type VolumeUsageConfigProvider interface {
	Get() config.SystemConfig
}

type VolumeUsageWorker struct {
	volRepo   VolumeUsageRepo
	contRepo  VolumeUsageContainerRepo
	imageRepo VolumeUsageImageRepo
	dockerAPI VolumeUsageDockerAPI
	users     UserInfoProvider
	cfg       VolumeUsageConfigProvider
	logger    *slog.Logger
}

func NewVolumeUsageWorker(volRepo VolumeUsageRepo, contRepo VolumeUsageContainerRepo, imageRepo VolumeUsageImageRepo, dockerAPI VolumeUsageDockerAPI, users UserInfoProvider, cfg VolumeUsageConfigProvider, logger *slog.Logger) *VolumeUsageWorker {
	return &VolumeUsageWorker{
		volRepo:   volRepo,
		contRepo:  contRepo,
		imageRepo: imageRepo,
		dockerAPI: dockerAPI,
		users:     users,
		cfg:       cfg,
		logger:    logging.WithComponent(logger, "volume_usage_worker"),
	}
}

func (w *VolumeUsageWorker) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "volume usage worker started")
	w.tick(ctx)

	for {
		interval := time.Duration(w.cfg.Get().EventSyncIntervalSeconds) * time.Second
		if interval <= 0 {
			interval = 30 * time.Second
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			w.logger.InfoContext(ctx, "volume usage worker stopped")
			return
		case <-timer.C:
			w.tick(ctx)
		}
	}
}

func (w *VolumeUsageWorker) tick(ctx context.Context) {
	w.refreshUsage(ctx)
	w.enforceQuotas(ctx)
}

func (w *VolumeUsageWorker) refreshUsage(ctx context.Context) {
	volumes, err := w.volRepo.GetReconcileCandidates(ctx)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to fetch volumes for usage refresh", "error", err)
		return
	}
	for _, vol := range volumes {
		usedBytes, err := w.dockerAPI.GetVolumeUsageBytes(ctx, vol.DockerName)
		if err != nil {
			w.logger.WarnContext(ctx, "failed to measure volume usage", "volume_id", vol.ID, "docker_name", vol.DockerName, "error", err)
			continue
		}
		if err := w.volRepo.UpdateUsage(ctx, vol.ID, usedBytes); err != nil {
			w.logger.WarnContext(ctx, "failed to update volume usage", "volume_id", vol.ID, "error", err)
		}
	}
}

func (w *VolumeUsageWorker) enforceQuotas(ctx context.Context) {
	owners, err := w.volRepo.GetOwnersWithVolumes(ctx)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to fetch volume owners", "error", err)
		return
	}
	for _, ownerID := range owners {
		user, err := w.users.GetUser(ctx, ownerID)
		if err != nil {
			w.logger.WarnContext(ctx, "failed to fetch user for volume quota enforcement", "user_id", ownerID, "error", err)
			continue
		}
		usedMB, err := w.imageRepo.GetUserUsedDiskSpace(ctx, ownerID)
		if err != nil {
			w.logger.WarnContext(ctx, "failed to fetch image disk usage", "user_id", ownerID, "error", err)
			continue
		}
		volumeBytes, err := w.volRepo.GetUserUsedVolumeBytes(ctx, ownerID)
		if err != nil {
			w.logger.WarnContext(ctx, "failed to fetch volume disk usage", "user_id", ownerID, "error", err)
			continue
		}
		usedMB += bytesToMBRoundedUp(volumeBytes)
		if usedMB < user.QuotaDiskMB {
			continue
		}
		w.stopWritableVolumeContainers(ctx, ownerID, usedMB, user.QuotaDiskMB)
	}
}

func (w *VolumeUsageWorker) stopWritableVolumeContainers(ctx context.Context, ownerID uuid.UUID, usedMB, quotaMB int64) {
	containers, err := w.contRepo.GetRunningWithWritableVolumeMounts(ctx, ownerID)
	if err != nil {
		w.logger.WarnContext(ctx, "failed to fetch over-quota containers", "user_id", ownerID, "error", err)
		return
	}
	for _, c := range containers {
		if err := w.queueStop(ctx, c); err != nil {
			if errors.Is(err, apperrors.ErrConflict) {
				w.logger.DebugContext(ctx, "over-quota container already has active operation", "container_id", c.ID)
				continue
			}
			w.logger.WarnContext(ctx, "failed to queue over-quota container stop", "container_id", c.ID, "error", err)
			continue
		}
		w.logger.WarnContext(ctx, "container stop queued due to disk quota", "container_id", c.ID, "user_id", ownerID, "used_mb", usedMB, "quota_mb", quotaMB)
	}
}

func (w *VolumeUsageWorker) queueStop(ctx context.Context, c model.Container) error {
	active, err := w.contRepo.HasActiveOperation(ctx, model.ResourceTypeContainer, c.ID)
	if err != nil {
		return err
	}
	if active {
		return apperrors.New(apperrors.ErrConflict, "resource operation is already in progress")
	}
	op := model.ResourceOperation{
		ID:           uuid.New(),
		ResourceType: model.ResourceTypeContainer,
		ResourceID:   c.ID,
		OwnerID:      c.OwnerID,
		Operation:    model.OperationStop,
		Status:       model.OperationStatusPending,
	}
	msg := containerqueue.LifecycleMessage{
		OperationID:    op.ID.String(),
		ContainerID:    c.ID.String(),
		OwnerID:        c.OwnerID.String(),
		Operation:      model.OperationStop,
		RequestID:      logging.RequestIDFromContext(ctx),
		CreatedAt:      time.Now().Unix(),
		PreviousStatus: c.Status,
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal over-quota stop message: %w", err)
	}
	outbox := model.ContainerLifecycleOutbox{
		ID:          uuid.New(),
		OperationID: op.ID,
		ContainerID: c.ID,
		Exchange:    containerqueue.ExchangeName,
		RoutingKey:  containerqueue.RoutingKey,
		Payload:     payload,
		Status:      model.ContainerOutboxStatusPending,
	}
	return w.contRepo.QueueContainerOperation(ctx, c.ID, model.ContainerStatusStopping, model.ContainerStatusExited, op, outbox)
}
