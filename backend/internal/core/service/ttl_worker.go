package service

import (
	"context"
	"log"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/google/uuid"
)

type TTLContainerRepository interface {
	GetExpired(ctx context.Context) ([]domain.Container, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
}

type TTLDockerAPI interface {
	StopContainer(ctx context.Context, dockerID string, timeout int) error
}

type TTLWorker struct {
	repo      TTLContainerRepository
	dockerAPI TTLDockerAPI
	interval  time.Duration
}

func NewTTLWorker(repo TTLContainerRepository, dockerAPI TTLDockerAPI, interval time.Duration) *TTLWorker {
	return &TTLWorker{
		repo:      repo,
		dockerAPI: dockerAPI,
		interval:  interval,
	}
}

func (w *TTLWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	log.Printf("TTL Worker started with interval %s", w.interval.String())

	for {
		select {
		case <-ctx.Done():
			log.Println("TTL Worker stopped")
			return
		case <-ticker.C:
			w.processExpired(ctx)
		}
	}
}

func (w *TTLWorker) processExpired(ctx context.Context) {
	expiredContainers, err := w.repo.GetExpired(ctx)
	if err != nil {
		log.Printf("[TTL Worker] Failed to fetch expired containers: %v", err)
		return
	}

	for _, c := range expiredContainers {
		log.Printf("[TTL Worker] Stopping expired container: %s (DockerID: %s)", c.ID, c.DockerID)

		// 1. Пытаемся остановить в Docker (таймаут 10 секунд на grace graceful shutdown)
		err := w.dockerAPI.StopContainer(ctx, c.DockerID, 10)
		if err != nil {
			log.Printf("[TTL Worker] Failed to stop container %s in docker: %v", c.ID, err)
			// Даже если докер вернул ошибку (например, контейнер уже "умер"),
			// мы все равно обновим статус в БД ниже, чтобы не пытаться остановить его бесконечно.
		}

		// 2. Обновляем статус в БД
		err = w.repo.UpdateStatus(ctx, c.ID, domain.ContainerStatusExited)
		if err != nil {
			log.Printf("[TTL Worker] Failed to update status in DB for container %s: %v", c.ID, err)
		}
	}
}
