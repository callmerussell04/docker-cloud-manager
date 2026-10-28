package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
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
	ScanVolumeUsage(ctx context.Context, volumeName string, scan model.VolumeUsageScan, maxEntries int) (model.VolumeUsageScan, error)
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

	// scans holds measurements not yet published and lastScanned is the
	// round-robin position by volume ID. Only Run's goroutine touches them.
	scans       map[uuid.UUID]pendingVolumeScan
	lastScanned uuid.UUID
}

// pendingVolumeScan is a measurement that is unfinished or failed to publish.
// Quota enforcement counts its bytes when they exceed the last published
// measurement. Files deleted after being counted can overstate usage until the
// pass completes, which errs toward enforcing the quota.
type pendingVolumeScan struct {
	ownerID        uuid.UUID
	publishedBytes int64
	scan           model.VolumeUsageScan
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
		scans:     map[uuid.UUID]pendingVolumeScan{},
	}
}

func (w *VolumeUsageWorker) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "volume usage worker started")
	w.tick(ctx)
	runPeriodically(ctx, func() int64 { return w.cfg.Get().VolumeUsageIntervalSeconds }, w.tick)
	w.logger.InfoContext(ctx, "volume usage worker stopped")
}

func (w *VolumeUsageWorker) tick(ctx context.Context) {
	w.refreshUsage(ctx)
	if ctx.Err() != nil {
		return
	}
	w.enforceQuotas(ctx)
}

// refreshUsage advances volume measurements within the cycle budget. Volumes
// are visited round-robin by ID, each for at most the per-volume entry budget.
// A volume the deadline interrupts after others ran first leads the next
// cycle, so it gets the whole budget before the rotation passes it; one
// interrupted while leading is passed, so it cannot hold the lead. Unfinished
// scans resume from their cursor; only complete scans are published.
func (w *VolumeUsageWorker) refreshUsage(ctx context.Context) {
	volumes, err := w.volRepo.GetReconcileCandidates(ctx)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to fetch volumes for usage refresh", "error", err)
		return
	}
	w.forgetRemovedScans(volumes)
	cfg := w.cfg.Get()
	budget := max(time.Duration(cfg.VolumeUsageCycleBudgetSeconds)*time.Second, time.Second)
	maxEntries := max(cfg.VolumeUsageScanEntryBudget, 1)
	cycleCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	for i, vol := range w.rotation(volumes) {
		if cycleCtx.Err() != nil {
			break
		}
		previous := w.lastScanned
		w.lastScanned = vol.ID
		scan, err := w.dockerAPI.ScanVolumeUsage(cycleCtx, vol.DockerName, w.scans[vol.ID].scan, maxEntries)
		// Kept until published, on error too: the next slice retries from the
		// last counted entry, and a complete scan is re-published.
		w.scans[vol.ID] = pendingVolumeScan{ownerID: vol.OwnerID, publishedBytes: vol.UsedBytes, scan: scan}
		if err != nil {
			if cycleCtx.Err() != nil {
				if i > 0 {
					w.lastScanned = previous
				}
				break
			}
			w.logger.WarnContext(ctx, "failed to measure volume usage", "volume_id", vol.ID, "docker_name", vol.DockerName, "error", err)
			continue
		}
		if !scan.Complete {
			continue
		}
		if err := w.volRepo.UpdateUsage(ctx, vol.ID, scan.Bytes); err != nil {
			w.logger.WarnContext(ctx, "failed to update volume usage", "volume_id", vol.ID, "error", err)
			continue
		}
		delete(w.scans, vol.ID)
	}
	if ctx.Err() == nil && cycleCtx.Err() != nil {
		w.logger.InfoContext(ctx, "volume usage cycle budget spent; unfinished scans resume next cycle", "budget_ms", budget.Milliseconds(), "unfinished_scans", len(w.scans))
	}
}

// rotation orders volumes by ID, starting after the last volume scanned.
func (w *VolumeUsageWorker) rotation(volumes []model.Volume) []model.Volume {
	sorted := slices.SortedFunc(slices.Values(volumes), func(a, b model.Volume) int {
		return compareVolumeIDs(a.ID, b.ID)
	})
	next, _ := slices.BinarySearchFunc(sorted, w.lastScanned, func(v model.Volume, id uuid.UUID) int {
		return compareVolumeIDs(v.ID, id)
	})
	if next < len(sorted) && sorted[next].ID == w.lastScanned {
		next++
	}
	return slices.Concat(sorted[next:], sorted[:next])
}

func compareVolumeIDs(a, b uuid.UUID) int {
	return bytes.Compare(a[:], b[:])
}

func (w *VolumeUsageWorker) forgetRemovedScans(volumes []model.Volume) {
	listed := make(map[uuid.UUID]struct{}, len(volumes))
	for _, vol := range volumes {
		listed[vol.ID] = struct{}{}
	}
	for id := range w.scans {
		if _, ok := listed[id]; !ok {
			delete(w.scans, id)
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
		usedMB += bytesToMBRoundedUp(volumeBytes + w.unpublishedBytes(ownerID))
		if usedMB < user.QuotaDiskMB {
			continue
		}
		w.stopWritableVolumeContainers(ctx, ownerID, usedMB, user.QuotaDiskMB)
	}
}

// unpublishedBytes is how far the owner's pending scans have already counted
// beyond the last published measurements of those volumes.
func (w *VolumeUsageWorker) unpublishedBytes(ownerID uuid.UUID) int64 {
	var excess int64
	for _, s := range w.scans {
		if s.ownerID == ownerID && s.scan.Bytes > s.publishedBytes {
			excess += s.scan.Bytes - s.publishedBytes
		}
	}
	return excess
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
