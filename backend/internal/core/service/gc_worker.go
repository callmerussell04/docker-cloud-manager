package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/google/uuid"
)

type GCDockerAPI interface {
	PruneSystem(ctx context.Context) error
	RunRegistryGarbageCollect(ctx context.Context, registryContainerName string) error // НОВЫЙ МЕТОД
}

type GCImageService interface {
	CompleteBuildRecord(ctx context.Context, buildID, imageID uuid.UUID, status string, sizeMB int) error
}

type GCBuildRepo interface {
	GetStaleBuilds(ctx context.Context, threshold time.Time) ([]model.Build, error)
}

type GCStagedObjectRepo interface {
	ListStaleActive(ctx context.Context, cutoff time.Time, limit int) ([]model.StagedObjectReservation, error)
	Release(ctx context.Context, objectKey string) error
}

type GCObjectStore interface {
	DeleteObject(ctx context.Context, objectKey string) error
}

type GCConfigProvider interface {
	Get() config.SystemConfig
}

type GCWorker struct {
	dockerAPI   GCDockerAPI
	imgSvc      GCImageService
	buildRepo   GCBuildRepo
	stagedRepo  GCStagedObjectRepo
	objectStore GCObjectStore
	cfg         GCConfigProvider
	logger      *slog.Logger
}

func NewGCWorker(dockerAPI GCDockerAPI, imgSvc GCImageService, buildRepo GCBuildRepo, stagedRepo GCStagedObjectRepo, objectStore GCObjectStore, cfg GCConfigProvider, logger *slog.Logger) *GCWorker {
	return &GCWorker{
		dockerAPI:   dockerAPI,
		imgSvc:      imgSvc,
		buildRepo:   buildRepo,
		stagedRepo:  stagedRepo,
		objectStore: objectStore,
		cfg:         cfg,
		logger:      logging.WithComponent(logger, "gc_worker"),
	}
}

func (w *GCWorker) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "gc worker started")
	w.runPrune(ctx)

	for {
		interval := time.Duration(w.cfg.Get().GCWorkerIntervalMinutes) * time.Minute
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			w.logger.InfoContext(ctx, "gc worker stopped")
			return
		case <-timer.C:
			w.runPrune(ctx)
		}
	}
}

func (w *GCWorker) runPrune(ctx context.Context) {
	// 1. Очистка самого Docker демона (останавливает накопление кэша и пустых слоев)
	err := w.dockerAPI.PruneSystem(ctx)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to prune docker system", "error", err)
	}

	// 2. Очистка локального Registry (физическое удаление "soft-deleted" манифестов)
	cfg := w.cfg.Get()
	if cfg.RegistryContainerName != "" {
		err = w.dockerAPI.RunRegistryGarbageCollect(ctx, cfg.RegistryContainerName)
		if err != nil {
			w.logger.ErrorContext(ctx, "failed to run registry garbage collection", "registry_container", cfg.RegistryContainerName, "error", err)
		} else {
			w.logger.InfoContext(ctx, "registry garbage collection completed", "registry_container", cfg.RegistryContainerName)
		}
	}

	// 3. Очистка зависших сборок
	threshold := time.Now().Add(-time.Duration(cfg.StaleBuildTimeoutMinutes) * time.Minute)
	staleBuilds, err := w.buildRepo.GetStaleBuilds(ctx, threshold)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to fetch stale builds", "error", err)
		return
	}

	for _, b := range staleBuilds {
		w.logger.WarnContext(ctx, "failing stale build", "build_id", b.ID, "image_id", b.ImageID)
		_ = w.imgSvc.CompleteBuildRecord(ctx, b.ID, b.ImageID, model.BuildStatusFailedTimeout, 0)
	}

	w.cleanupStaleStagedObjects(ctx)
}

func (w *GCWorker) cleanupStaleStagedObjects(ctx context.Context) {
	if w.stagedRepo == nil || w.objectStore == nil {
		return
	}
	cutoff := time.Now().Add(-time.Duration(w.cfg.Get().StaleBuildTimeoutMinutes) * time.Minute)
	items, err := w.stagedRepo.ListStaleActive(ctx, cutoff, 100)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to fetch stale staged object reservations", "error", err)
		return
	}
	for _, item := range items {
		if err := w.objectStore.DeleteObject(ctx, item.ObjectKey); err != nil {
			w.logger.WarnContext(ctx, "failed to delete stale staged object", "object_key", item.ObjectKey, "error", err)
			continue
		}
		if err := w.stagedRepo.Release(ctx, item.ObjectKey); err != nil {
			w.logger.WarnContext(ctx, "failed to release stale staged object reservation", "object_key", item.ObjectKey, "error", err)
		}
	}
}
