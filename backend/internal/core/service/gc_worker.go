package service

import (
	"context"
	"log/slog"
	"time"

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

type GCWorker struct {
	dockerAPI             GCDockerAPI
	imgSvc                GCImageService
	buildRepo             GCBuildRepo
	interval              time.Duration
	buildTimeout          time.Duration
	registryContainerName string
	logger                *slog.Logger
}

func NewGCWorker(dockerAPI GCDockerAPI, imgSvc GCImageService, buildRepo GCBuildRepo, interval, buildTimeout time.Duration, registryContainerName string, logger *slog.Logger) *GCWorker {
	return &GCWorker{
		dockerAPI:             dockerAPI,
		imgSvc:                imgSvc,
		buildRepo:             buildRepo,
		interval:              interval,
		buildTimeout:          buildTimeout,
		registryContainerName: registryContainerName,
		logger:                logging.WithComponent(logger, "gc_worker"),
	}
}

func (w *GCWorker) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "gc worker started", "interval", w.interval.String())
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.InfoContext(ctx, "gc worker stopped")
			return
		case <-ticker.C:
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
	if w.registryContainerName != "" {
		err = w.dockerAPI.RunRegistryGarbageCollect(ctx, w.registryContainerName)
		if err != nil {
			w.logger.ErrorContext(ctx, "failed to run registry garbage collection", "registry_container", w.registryContainerName, "error", err)
		} else {
			w.logger.InfoContext(ctx, "registry garbage collection completed", "registry_container", w.registryContainerName)
		}
	}

	// 3. Очистка зависших сборок
	threshold := time.Now().Add(-w.buildTimeout)
	staleBuilds, err := w.buildRepo.GetStaleBuilds(ctx, threshold)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to fetch stale builds", "error", err)
		return
	}

	for _, b := range staleBuilds {
		w.logger.WarnContext(ctx, "failing stale build", "build_id", b.ID, "image_id", b.ImageID)
		_ = w.imgSvc.CompleteBuildRecord(ctx, b.ID, b.ImageID, "failed_timeout", 0)
	}
}
