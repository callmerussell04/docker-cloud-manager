package service

import (
	"context"
	"log"
	"time"
)

type GCDockerAPI interface {
	PruneSystem(ctx context.Context) error
}

type GCWorker struct {
	dockerAPI GCDockerAPI
	interval  time.Duration
}

func NewGCWorker(dockerAPI GCDockerAPI, interval time.Duration) *GCWorker {
	return &GCWorker{
		dockerAPI: dockerAPI,
		interval:  interval,
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
	err := w.dockerAPI.PruneSystem(ctx)
	if err != nil {
		log.Printf("[GC Worker] Failed to prune docker system: %v", err)
		return
	}
}
