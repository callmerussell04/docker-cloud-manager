package service

import (
	"context"
	"log"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
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
}

func NewGCWorker(dockerAPI GCDockerAPI, imgSvc GCImageService, buildRepo GCBuildRepo, interval, buildTimeout time.Duration, registryContainerName string) *GCWorker {
	return &GCWorker{
		dockerAPI:             dockerAPI,
		imgSvc:                imgSvc,
		buildRepo:             buildRepo,
		interval:              interval,
		buildTimeout:          buildTimeout,
		registryContainerName: registryContainerName,
	}
}

func (w *GCWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
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
		log.Printf("[GC Worker] Failed to prune docker system: %v", err)
	}

	// 2. Очистка локального Registry (физическое удаление "soft-deleted" манифестов)
	if w.registryContainerName != "" {
		err = w.dockerAPI.RunRegistryGarbageCollect(ctx, w.registryContainerName)
		if err != nil {
			log.Printf("[GC Worker] Failed to run registry garbage collection: %v", err)
		} else {
			log.Printf("[GC Worker] Registry garbage collection completed successfully")
		}
	}

	// 3. Очистка зависших сборок
	threshold := time.Now().Add(-w.buildTimeout)
	staleBuilds, err := w.buildRepo.GetStaleBuilds(ctx, threshold)
	if err != nil {
		log.Printf("[GC Worker] Failed to fetch stale builds: %v", err)
		return
	}

	for _, b := range staleBuilds {
		log.Printf("[GC Worker] Failing stale build: %s", b.ID)
		_ = w.imgSvc.CompleteBuildRecord(ctx, b.ID, b.ImageID, "failed_timeout", 0)
	}
}
