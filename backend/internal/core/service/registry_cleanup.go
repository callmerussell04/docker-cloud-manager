package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/nilcheck"
	"github.com/google/uuid"
)

type RegistryCleanupRepository interface {
	PendingRegistryCleanup(ctx context.Context) ([]model.RegistryCleanup, error)
	LockRegistryRepository(ctx context.Context, repository string) (func(), error)
	RegistryCleanupFacts(ctx context.Context, job model.RegistryCleanup, fence time.Duration) (model.RegistryCleanupFacts, error)
	CompleteRegistryCleanup(ctx context.Context, id uuid.UUID) error
}

type RegistryCleanupConfigProvider interface {
	Get() config.SystemConfig
}

// RegistryCleanupWorker deletes the Registry tags that revoked builds queued
// for removal once their issued tokens can no longer write.
type RegistryCleanupWorker struct {
	repo     RegistryCleanupRepository
	images   ManifestSharingRepository
	registry ImageRegistryAPI
	cfg      RegistryCleanupConfigProvider
	logger   *slog.Logger
}

// RegistryCleanupWorkerDeps requires everything but the logger. The Registry
// API is built after RegistryAuthorization, whose trusted tokens it uses.
type RegistryCleanupWorkerDeps struct {
	Repository RegistryCleanupRepository
	// Images keeps a manifest that another available image shares.
	Images   ManifestSharingRepository
	Registry ImageRegistryAPI
	Config   RegistryCleanupConfigProvider
	Logger   *slog.Logger
}

func NewRegistryCleanupWorker(deps RegistryCleanupWorkerDeps) (*RegistryCleanupWorker, error) {
	required := []struct {
		name  string
		value any
	}{
		{"repository", deps.Repository}, {"images", deps.Images}, {"registry", deps.Registry}, {"config", deps.Config},
	}
	for _, dependency := range required {
		if nilcheck.IsNil(dependency.value) {
			return nil, fmt.Errorf("registry cleanup worker: %s is required", dependency.name)
		}
	}
	return &RegistryCleanupWorker{
		repo:     deps.Repository,
		images:   deps.Images,
		registry: deps.Registry,
		cfg:      deps.Config,
		logger:   logging.WithComponent(deps.Logger, "registry_cleanup_worker"),
	}, nil
}

// Run deletes fenced Registry tags. Interval and per-job timeout are read
// fresh before each tick; a failed poll or job is logged here once and the job
// stays durable, so a later tick retries it.
func (w *RegistryCleanupWorker) Run(ctx context.Context) {
	runPeriodically(ctx, func() int64 { return w.cfg.Get().RegistryCleanupIntervalSeconds }, func(ctx context.Context) {
		w.cleanupTick(ctx, registryLockBudget(w.cfg.Get()))
	})
}

func (w *RegistryCleanupWorker) cleanupTick(ctx context.Context, timeout time.Duration) {
	jobs, err := w.repo.PendingRegistryCleanup(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.ErrorContext(ctx, "registry cleanup poll failed", "operation", "registry cleanup poll", "error", err)
		}
		return
	}
	for _, job := range jobs {
		jobCtx, cancel := context.WithTimeout(ctx, timeout)
		err := w.cleanupJob(jobCtx, job)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			w.logger.ErrorContext(ctx, "registry cleanup job failed", "operation", "registry cleanup job", "cleanup_id", job.ID, "error", err)
		}
	}
}

func (w *RegistryCleanupWorker) cleanupJob(ctx context.Context, job model.RegistryCleanup) error {
	unlock, err := w.repo.LockRegistryRepository(ctx, job.Repository)
	if err != nil {
		return fmt.Errorf("lock registry repository: %w", err)
	}
	defer unlock()
	facts, err := w.repo.RegistryCleanupFacts(ctx, job, RegistryCleanupDelay)
	if err != nil {
		return fmt.Errorf("read registry cleanup state: %w", err)
	}
	switch registryCleanupState(facts) {
	case model.RegistryCleanupPending:
		return nil
	case model.RegistryCleanupReady:
		// A cleanup job belongs to no available image, so none is excluded.
		if err := deleteUnsharedManifest(ctx, w.registry, w.images, job.Repository, job.Tag, uuid.Nil); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
			return fmt.Errorf("delete registry manifest: %w", err)
		}
	}
	if err := w.repo.CompleteRegistryCleanup(ctx, job.ID); err != nil {
		return fmt.Errorf("complete registry cleanup: %w", err)
	}
	return nil
}

// registryCleanupState decides a job from facts read under the repository
// lock. Repository tokens cover every tag, so any build that may still publish
// to the repository postpones deletion; a tag an available image owns again is
// no longer the revoked build's to delete.
func registryCleanupState(facts model.RegistryCleanupFacts) model.RegistryCleanupState {
	switch {
	case facts.PublisherActive || facts.TokenMayWrite:
		return model.RegistryCleanupPending
	case facts.TagAvailable:
		return model.RegistryCleanupObsolete
	default:
		return model.RegistryCleanupReady
	}
}
