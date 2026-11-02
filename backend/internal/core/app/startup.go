package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/internal/platform/pgpool"
)

// openPostgresPools opens the query pool and the single-connection lock pool
// with Core's split budget. On success the caller owns and closes both.
func openPostgresPools(dsn string, budget pgpool.Config) (*sql.DB, *sql.DB, error) {
	queryCfg, lockCfg, err := budget.CoreBudgets()
	if err != nil {
		return nil, nil, err
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("open postgres pool: %w", err)
	}
	if err := queryCfg.Apply(db); err != nil {
		_ = db.Close() // startup failed and its error is returned; the pool is never used
		return nil, nil, err
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close() // startup failed and its error is returned; the pool is never used
		return nil, nil, fmt.Errorf("ping postgres pool: %w", err)
	}
	lockDB, err := sql.Open("postgres", dsn)
	if err != nil {
		_ = db.Close() // startup failed and its error is returned; the pool is never used
		return nil, nil, fmt.Errorf("open postgres lock pool: %w", err)
	}
	if err := lockCfg.Apply(lockDB); err != nil {
		_ = db.Close()     // startup failed and its error is returned; the pool is never used
		_ = lockDB.Close() // startup failed and its error is returned; the pool is never used
		return nil, nil, err
	}
	return db, lockDB, nil
}

// recoverInterruptedWork settles work a previous Core process left running
// before publishers and consumers start. Only resource operation recovery is
// fatal; the other paths are logged and retried by their workers.
func recoverInterruptedWork(ctx context.Context, orchestrator *compose.Orchestrator, projects *repository.ProjectRepository, resources *repository.ResourceLifecycleRepository, containers *repository.ContainerRepository, runtime *config.Manager, logger *slog.Logger) error {
	recoveryMessage := "deployment interrupted by core service restart"
	if err := orchestrator.CleanupInterruptedDeployments(ctx, recoveryMessage); err != nil {
		logger.Warn("failed to cleanup interrupted compose deployments", "error", err)
	}
	if err := projects.RecoverInterruptedComposeDeployments(ctx, runtime.Get().ComposeDeployMaxAttempts, recoveryMessage); err != nil {
		logger.Warn("failed to recover interrupted compose deployments", "error", err)
	}
	operationCause := errors.New("operation interrupted by core service restart")
	if recovered, err := resources.RecoverInterruptedOperations(ctx, operationCause); err != nil {
		return fmt.Errorf("recover interrupted resource operations: %w", err)
	} else if recovered > 0 {
		logger.Warn("requeued interrupted volume and image operations", "count", recovered)
	}
	if recovered, err := containers.RecoverInterruptedContainerCreates(ctx, operationCause); err != nil {
		logger.Warn("failed to recover interrupted container create operations", "error", err)
	} else if recovered > 0 {
		logger.Warn("requeued interrupted container create operations", "count", recovered)
	}
	if recovered, err := containers.FailInterruptedNonResumableOperations(ctx, operationCause); err != nil {
		logger.Warn("failed to mark interrupted non-resumable operations", "error", err)
	} else if recovered > 0 {
		logger.Warn("failed interrupted non-resumable operations", "count", recovered)
	}
	return nil
}

// outboxWorkers relays the four durable outboxes through one shared publisher.
func outboxWorkers(publisher service.OutboxPublisher, builds *repository.BuildRepository, projects *repository.ProjectRepository, containers *repository.ContainerRepository, resources *repository.ResourceLifecycleRepository, runtime *config.Manager, logger *slog.Logger) []func(context.Context) {
	return []func(context.Context){
		service.NewBuildOutboxWorker(builds, publisher, runtime, logger).Run,
		service.NewComposeOutboxWorker(projects, publisher, runtime, logger).Run,
		service.NewContainerOutboxWorker(containers, publisher, runtime, logger).Run,
		service.NewResourceOutboxWorker(resources, publisher, runtime, logger).Run,
	}
}
