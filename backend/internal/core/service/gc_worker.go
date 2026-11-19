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

// GCDockerAPI starts Registry garbage collection as an exec in the Registry
// container and reports whether it still runs, so the worker can renew its
// lease while it waits. Inspecting an exec that no longer exists returns
// apperrors.ErrNotFound.
type GCDockerAPI interface {
	PruneSystem(ctx context.Context) error
	StartRegistryGarbageCollect(ctx context.Context, registryContainerName string) (execID string, err error)
	InspectRegistryGarbageCollect(ctx context.Context, execID string) (running bool, exitCode int, err error)
}

type GCImageService interface {
	CompleteBuildRecord(ctx context.Context, buildID, imageID uuid.UUID, status string, sizeMB int) error
}

type GCBuildRepo interface {
	GetStaleBuilds(ctx context.Context, threshold time.Time) ([]model.Build, error)
}

// GCRegistryGate is the lease that keeps Registry garbage collection and build
// claims apart: no build is claimed while it is held. RenewLease extends only
// a lease that is still live and reports whether it did.
type GCRegistryGate interface {
	TryAcquireLease(ctx context.Context, lease, drainAfter time.Duration) (model.RegistryGCAttempt, error)
	RenewLease(ctx context.Context, lease time.Duration) (renewed bool, err error)
	ReleaseLease(ctx context.Context, completed bool) error
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

const (
	// registryGCRetryDelay is how soon a collection skipped for running
	// builds is tried again.
	registryGCRetryDelay = time.Minute
	// registryGCDrainAfter is how long collection may be skipped before new
	// builds are held back until the running ones finish.
	registryGCDrainAfter = 24 * time.Hour
	// registryGCPollInterval is how often a running collection is inspected.
	registryGCPollInterval = time.Second
	// registryGCRenewAhead is how long before the lease deadline a running
	// collection renews it.
	registryGCRenewAhead = time.Minute
)

// GCWorkerDeps are the GC worker's dependencies. StagedObjects and Objects are
// optional: without both, stale staged objects are not cleaned.
type GCWorkerDeps struct {
	Docker        GCDockerAPI
	Images        GCImageService
	Builds        GCBuildRepo
	RegistryGC    GCRegistryGate
	StagedObjects GCStagedObjectRepo
	Objects       GCObjectStore
	Config        GCConfigProvider
	Logger        *slog.Logger
}

type GCWorker struct {
	dockerAPI   GCDockerAPI
	imgSvc      GCImageService
	buildRepo   GCBuildRepo
	registryGC  GCRegistryGate
	stagedRepo  GCStagedObjectRepo
	objectStore GCObjectStore
	cfg         GCConfigProvider
	logger      *slog.Logger
	// draining remembers that the last attempt found builds held back, so
	// the start of draining is logged once.
	draining bool
}

func NewGCWorker(deps GCWorkerDeps) (*GCWorker, error) {
	required := []struct {
		name  string
		value any
	}{
		{"docker", deps.Docker}, {"images", deps.Images}, {"builds", deps.Builds},
		{"registry gc", deps.RegistryGC}, {"config", deps.Config},
	}
	for _, dependency := range required {
		if nilcheck.IsNil(dependency.value) {
			return nil, fmt.Errorf("gc worker: %s is required", dependency.name)
		}
	}
	w := &GCWorker{
		dockerAPI:  deps.Docker,
		imgSvc:     deps.Images,
		buildRepo:  deps.Builds,
		registryGC: deps.RegistryGC,
		cfg:        deps.Config,
		logger:     logging.WithComponent(deps.Logger, "gc_worker"),
	}
	if !nilcheck.IsNil(deps.StagedObjects) && !nilcheck.IsNil(deps.Objects) {
		w.stagedRepo, w.objectStore = deps.StagedObjects, deps.Objects
	}
	return w, nil
}

// Run prunes Docker, fails stale builds and cleans stale staged objects every
// interval, then tries Registry garbage collection. A collection skipped
// because builds run is tried again every minute until it runs.
func (w *GCWorker) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "gc worker started")
	retry := w.runPrune(ctx)

	interval := time.NewTimer(w.interval())
	defer interval.Stop()
	retryTimer := time.NewTimer(registryGCRetryDelay)
	defer retryTimer.Stop()
	for {
		if !retry {
			retryTimer.Stop()
		}
		select {
		case <-ctx.Done():
			w.logger.InfoContext(ctx, "gc worker stopped")
			return
		case <-interval.C:
			retry = w.runPrune(ctx)
			interval.Reset(w.interval())
		case <-retryTimer.C:
			retry = w.collectRegistryGarbage(ctx)
		}
		if retry {
			retryTimer.Reset(registryGCRetryDelay)
		}
	}
}

func (w *GCWorker) interval() time.Duration {
	return time.Duration(w.cfg.Get().GCWorkerIntervalMinutes) * time.Minute
}

// runPrune runs one full pass and reports whether Registry GC must be retried.
// Stale builds are failed before collection is tried, because a stale running
// build would hold it back.
func (w *GCWorker) runPrune(ctx context.Context) bool {
	// 1. Очистка самого Docker демона (останавливает накопление кэша и пустых слоев)
	if err := w.dockerAPI.PruneSystem(ctx); err != nil {
		w.logger.ErrorContext(ctx, "failed to prune docker system", "error", err)
	}
	// 2. Очистка зависших сборок
	w.failStaleBuilds(ctx)
	w.cleanupStaleStagedObjects(ctx)
	// 3. Очистка локального Registry (физическое удаление "soft-deleted" манифестов)
	return w.collectRegistryGarbage(ctx)
}

// staleBuildHeadroom is what a build gets beyond its time budget, to finalize
// and deliver its result, before the GC pass may fail it as stale.
const staleBuildHeadroom = 10 * time.Minute

// staleBuildAge is how long ago a build must have started, or a staged source
// been reserved, for the GC pass to treat it as stale: the configured
// threshold, and never less than the build time budget plus headroom, so a
// raised budget cannot make the pass fail a build Builder is still running.
func staleBuildAge(cfg config.SystemConfig) time.Duration {
	return max(
		time.Duration(cfg.StaleBuildTimeoutMinutes)*time.Minute,
		time.Duration(cfg.MaxBuildTimeMinutes)*time.Minute+staleBuildHeadroom,
	)
}

func (w *GCWorker) failStaleBuilds(ctx context.Context) {
	threshold := time.Now().Add(-staleBuildAge(w.cfg.Get()))
	staleBuilds, err := w.buildRepo.GetStaleBuilds(ctx, threshold)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to fetch stale builds", "error", err)
		return
	}
	for _, b := range staleBuilds {
		w.logger.WarnContext(ctx, "failing stale build", "build_id", b.ID, "image_id", b.ImageID)
		if err := w.imgSvc.CompleteBuildRecord(ctx, b.ID, b.ImageID, model.BuildStatusFailedTimeout, 0); err != nil {
			w.logger.ErrorContext(ctx, "failed to fail stale build", "build_id", b.ID, "image_id", b.ImageID, "error", err)
		}
	}
}

// registryGCOutcome is how a collection run ended for its lease.
type registryGCOutcome int

const (
	// registryGCFinished: the process was observed finished, with any exit
	// code. The lease is released as a completed collection.
	registryGCFinished registryGCOutcome = iota
	// registryGCEnded: the process never started or is gone without an
	// observed end. The lease is released; the drain clock is not reset.
	registryGCEnded
	// registryGCUntracked: shutdown, or the lease lapsed before renewal.
	// The lease is left to its deadline.
	registryGCUntracked
)

// collectRegistryGarbage runs Registry garbage collection under the lease and
// reports whether it must be retried soon: builds still run, the lease could
// not be read or released, or it lapsed while collection ran.
func (w *GCWorker) collectRegistryGarbage(ctx context.Context) bool {
	cfg := w.cfg.Get()
	if cfg.RegistryContainerName == "" {
		return false
	}
	lease := time.Duration(cfg.RegistryGCMaxDurationSeconds) * time.Second
	acquiredAt := time.Now()
	attempt, err := w.registryGC.TryAcquireLease(ctx, lease, registryGCDrainAfter)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to take registry garbage collection lease", "error", err)
		return true
	}
	if attempt.Draining && !w.draining {
		w.logger.WarnContext(ctx, "registry garbage collection is overdue; new builds wait until running builds finish")
	}
	w.draining = attempt.Draining
	if !attempt.Acquired {
		w.logger.DebugContext(ctx, "registry garbage collection deferred while builds run")
		return true
	}

	outcome, err := w.runRegistryGarbageCollect(ctx, cfg.RegistryContainerName, lease, acquiredAt)
	switch {
	case outcome == registryGCUntracked:
		if ctx.Err() != nil {
			return false
		}
		w.logger.ErrorContext(ctx, "registry garbage collection lease lapsed before it was renewed", "registry_container", cfg.RegistryContainerName, "error", err)
		return true
	case err != nil:
		w.logger.ErrorContext(ctx, "registry garbage collection failed", "registry_container", cfg.RegistryContainerName, "error", err)
	default:
		w.logger.InfoContext(ctx, "registry garbage collection completed", "registry_container", cfg.RegistryContainerName)
	}
	if err := w.registryGC.ReleaseLease(ctx, outcome == registryGCFinished); err != nil {
		w.logger.ErrorContext(ctx, "failed to release registry garbage collection lease", "error", err)
		return true
	}
	w.draining = false
	return false
}

var errRegistryGCLeaseLapsed = errors.New("registry garbage collection lease lapsed")

// runRegistryGarbageCollect starts the collection and waits for it, renewing
// the lease ahead of each deadline. An exec that cannot be inspected is
// presumed running and keeps its lease while the lease stays live.
func (w *GCWorker) runRegistryGarbageCollect(ctx context.Context, registryContainer string, lease time.Duration, acquiredAt time.Time) (registryGCOutcome, error) {
	execID, err := w.dockerAPI.StartRegistryGarbageCollect(ctx, registryContainer)
	if err != nil {
		return registryGCEnded, fmt.Errorf("start registry garbage collection: %w", err)
	}
	budgetEnds := acquiredAt.Add(lease)
	leaseEnds := budgetEnds
	renewAhead := min(lease/2, registryGCRenewAhead)
	poll := time.NewTicker(min(registryGCPollInterval, renewAhead/2))
	defer poll.Stop()
	var overran, inspectFailing, renewFailing bool
	for {
		running, exitCode, err := w.dockerAPI.InspectRegistryGarbageCollect(ctx, execID)
		switch {
		case ctx.Err() != nil:
			return registryGCUntracked, ctx.Err()
		case errors.Is(err, apperrors.ErrNotFound):
			return registryGCEnded, fmt.Errorf("registry garbage collection exec is gone: %w", err)
		case err != nil:
			if !inspectFailing {
				w.logger.WarnContext(ctx, "cannot inspect registry garbage collection; presuming it still runs", "error", err)
			}
			inspectFailing = true
		case !running:
			if exitCode != 0 {
				return registryGCFinished, fmt.Errorf("registry garbage collection exited with code %d", exitCode)
			}
			return registryGCFinished, nil
		default:
			inspectFailing = false
		}

		now := time.Now()
		if !overran && !now.Before(budgetEnds) {
			overran = true
			w.logger.ErrorContext(ctx, "registry garbage collection runs past its time budget; builds keep waiting", "budget_seconds", int64(lease/time.Second))
		}
		if !now.Before(leaseEnds.Add(-renewAhead)) {
			renewed, err := w.registryGC.RenewLease(ctx, lease)
			switch {
			case err != nil:
				if !renewFailing {
					w.logger.WarnContext(ctx, "failed to renew registry garbage collection lease", "error", err)
				}
				renewFailing = true
			case !renewed:
				return registryGCUntracked, errRegistryGCLeaseLapsed
			default:
				renewFailing = false
				leaseEnds = now.Add(lease)
			}
		}
		select {
		case <-ctx.Done():
			return registryGCUntracked, ctx.Err()
		case <-poll.C:
		}
	}
}

func (w *GCWorker) cleanupStaleStagedObjects(ctx context.Context) {
	if w.stagedRepo == nil || w.objectStore == nil {
		return
	}
	cutoff := time.Now().Add(-staleBuildAge(w.cfg.Get()))
	items, err := w.stagedRepo.ListStaleActive(ctx, cutoff, 100)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to fetch stale staged object reservations", "error", err)
		return
	}
	for _, item := range items {
		if err := w.objectStore.DeleteObject(ctx, item.ObjectKey); err != nil {
			w.logger.WarnContext(ctx, "failed to delete stale staged object", "reservation_id", item.ID, "owner_id", item.OwnerID, "kind", item.Kind, "error", err)
			continue
		}
		if err := w.stagedRepo.Release(ctx, item.ObjectKey); err != nil {
			w.logger.WarnContext(ctx, "failed to release stale staged object reservation", "reservation_id", item.ID, "owner_id", item.OwnerID, "kind", item.Kind, "error", err)
		}
	}
}
