package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/google/uuid"
)

// VolumeSizeStopReason is the last error of a container the volume usage
// worker stopped because a volume it mounts writable grew beyond its size.
const VolumeSizeStopReason = "stopped by the platform: a volume this container can write to grew beyond its size"

type VolumeUsageRepo interface {
	GetReconcileCandidates(ctx context.Context) ([]model.Volume, error)
	UpdateUsage(ctx context.Context, id uuid.UUID, usedBytes int64) error
}

type VolumeUsageDockerAPI interface {
	ScanVolumeUsage(ctx context.Context, volumeName string, scan model.VolumeUsageScan, maxEntries int) (model.VolumeUsageScan, error)
}

type VolumeUsageContainerRepo interface {
	ListRunningWritersOfVolume(ctx context.Context, volumeID uuid.UUID) ([]model.Container, error)
	HasActiveOperation(ctx context.Context, resourceType string, resourceID uuid.UUID) (bool, error)
	QueueContainerOperation(ctx context.Context, id uuid.UUID, status string, desiredStatus string, op model.ResourceOperation, outbox model.ContainerLifecycleOutbox) error
}

type VolumeUsageConfigProvider interface {
	Get() config.SystemConfig
}

type VolumeUsageWorker struct {
	volRepo   VolumeUsageRepo
	contRepo  VolumeUsageContainerRepo
	dockerAPI VolumeUsageDockerAPI
	cfg       VolumeUsageConfigProvider
	logger    *slog.Logger

	// scans holds measurements that are unfinished or failed to publish, and
	// lastScanned is the round-robin position by volume ID. Only Run's
	// goroutine touches them.
	scans       map[uuid.UUID]model.VolumeUsageScan
	lastScanned uuid.UUID
}

func NewVolumeUsageWorker(volRepo VolumeUsageRepo, contRepo VolumeUsageContainerRepo, dockerAPI VolumeUsageDockerAPI, cfg VolumeUsageConfigProvider, logger *slog.Logger) *VolumeUsageWorker {
	return &VolumeUsageWorker{
		volRepo:   volRepo,
		contRepo:  contRepo,
		dockerAPI: dockerAPI,
		cfg:       cfg,
		logger:    logging.WithComponent(logger, "volume_usage_worker"),
		scans:     map[uuid.UUID]model.VolumeUsageScan{},
	}
}

func (w *VolumeUsageWorker) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "volume usage worker started")
	w.tick(ctx)
	runPeriodically(ctx, func() int64 { return w.cfg.Get().VolumeUsageIntervalSeconds }, w.tick)
	w.logger.InfoContext(ctx, "volume usage worker stopped")
}

func (w *VolumeUsageWorker) tick(ctx context.Context) {
	volumes := w.refreshUsage(ctx)
	if ctx.Err() != nil {
		return
	}
	w.enforceSizes(ctx, volumes)
}

// refreshUsage advances volume measurements within the cycle budget. Volumes
// are visited round-robin by ID, each for at most the per-volume entry budget.
// A volume the deadline interrupts after others ran first leads the next
// cycle, so it gets the whole budget before the rotation passes it; one
// interrupted while leading is passed, so it cannot hold the lead. Unfinished
// scans resume from their cursor; only complete scans are published. It
// returns the volumes with the measurements it published.
func (w *VolumeUsageWorker) refreshUsage(ctx context.Context) []model.Volume {
	volumes, err := w.volRepo.GetReconcileCandidates(ctx)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to fetch volumes for usage refresh", "error", err)
		return nil
	}
	w.forgetRemovedScans(volumes)
	cfg := w.cfg.Get()
	budget := max(time.Duration(cfg.VolumeUsageCycleBudgetSeconds)*time.Second, time.Second)
	maxEntries := max(cfg.VolumeUsageScanEntryBudget, 1)
	cycleCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	published := map[uuid.UUID]int64{}

	for i, vol := range w.rotation(volumes) {
		if cycleCtx.Err() != nil {
			break
		}
		previous := w.lastScanned
		w.lastScanned = vol.ID
		scan, err := w.dockerAPI.ScanVolumeUsage(cycleCtx, vol.DockerName, w.scans[vol.ID], maxEntries)
		// Kept until published, on error too: the next slice retries from the
		// last counted entry, and a complete scan is re-published.
		w.scans[vol.ID] = scan
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
		published[vol.ID] = scan.Bytes
	}
	if ctx.Err() == nil && cycleCtx.Err() != nil {
		w.logger.InfoContext(ctx, "volume usage cycle budget spent; unfinished scans resume next cycle", "budget_ms", budget.Milliseconds(), "unfinished_scans", len(w.scans))
	}
	for i := range volumes {
		if bytes, ok := published[volumes[i].ID]; ok {
			volumes[i].UsedBytes = bytes
		}
	}
	return volumes
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

// enforceSizes stops the running containers that can write to a volume whose
// usage is above its size. Each volume is judged by its own size alone.
func (w *VolumeUsageWorker) enforceSizes(ctx context.Context, volumes []model.Volume) {
	for _, vol := range volumes {
		if ctx.Err() != nil {
			return
		}
		usage := w.measuredBytes(vol)
		if usage <= vol.SizeBytes {
			continue
		}
		w.stopVolumeWriters(ctx, vol, usage)
	}
}

// measuredBytes is the larger of a volume's last published measurement and
// what its unfinished or unpublished scan has counted so far. Files deleted
// after being counted can overstate usage until the scan completes, which errs
// toward stopping writers.
func (w *VolumeUsageWorker) measuredBytes(vol model.Volume) int64 {
	return max(vol.UsedBytes, w.scans[vol.ID].Bytes)
}

func (w *VolumeUsageWorker) stopVolumeWriters(ctx context.Context, vol model.Volume, usage int64) {
	containers, err := w.contRepo.ListRunningWritersOfVolume(ctx, vol.ID)
	if err != nil {
		w.logger.WarnContext(ctx, "failed to fetch writers of oversized volume", "volume_id", vol.ID, "error", err)
		return
	}
	for _, c := range containers {
		if err := queueContainerStop(ctx, w.contRepo, c, VolumeSizeStopReason); err != nil {
			if errors.Is(err, apperrors.ErrConflict) {
				w.logger.DebugContext(ctx, "writer of oversized volume already has active operation", "container_id", c.ID, "volume_id", vol.ID)
				continue
			}
			w.logger.WarnContext(ctx, "failed to queue stop of oversized volume writer", "container_id", c.ID, "volume_id", vol.ID, "error", err)
			continue
		}
		w.logger.WarnContext(ctx, "container stop queued: a volume it writes to grew beyond its size",
			"container_id", c.ID, "volume_id", vol.ID, "user_id", vol.OwnerID, "used_bytes", usage, "size_bytes", vol.SizeBytes)
	}
}
