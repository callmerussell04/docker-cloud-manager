package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/google/uuid"
)

type VolumeUsageRepo interface {
	GetReconcileCandidates(ctx context.Context) ([]model.Volume, error)
	UpdateUsage(ctx context.Context, id uuid.UUID, usedBytes int64) error
	MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error
	GetOwnersWithVolumes(ctx context.Context) ([]uuid.UUID, error)
	GetUserUsedVolumeBytes(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

type VolumeUsageDockerAPI interface {
	GetVolumeUsageBytes(ctx context.Context, volumeName string) (int64, error)
	StopContainer(ctx context.Context, dockerID string, timeout int) error
}

type VolumeUsageContainerRepo interface {
	GetRunningWithWritableVolumeMounts(ctx context.Context, ownerID uuid.UUID) ([]model.Container, error)
	SetDesiredStatus(ctx context.Context, id uuid.UUID, desiredStatus string) error
	MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error
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
			_ = w.volRepo.MarkStatusError(ctx, vol.ID, vol.Status, err)
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
	cause := errors.New("user disk quota exceeded")
	for _, c := range containers {
		if err := w.contRepo.SetDesiredStatus(ctx, c.ID, model.ContainerStatusExited); err != nil {
			w.logger.WarnContext(ctx, "failed to update over-quota container desired status", "container_id", c.ID, "error", err)
		}
		if c.DockerID != "" {
			if err := w.dockerAPI.StopContainer(ctx, c.DockerID, w.cfg.Get().ContainerStopTimeout); err != nil {
				w.logger.WarnContext(ctx, "failed to stop over-quota container", "container_id", c.ID, "docker_id", c.DockerID, "error", err)
			}
		}
		_ = w.contRepo.MarkStatusError(ctx, c.ID, model.ContainerStatusExited, cause)
		w.logger.WarnContext(ctx, "container stopped due to disk quota", "container_id", c.ID, "user_id", ownerID, "used_mb", usedMB, "quota_mb", quotaMB)
	}
}
