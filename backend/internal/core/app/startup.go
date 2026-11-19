package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/pgpool"
	"github.com/jackc/pgx/v5/pgxpool"
)

// openPostgresPools opens the query pool and the single-connection lock pool
// with Core's split budget. On success the caller owns and closes both.
func openPostgresPools(ctx context.Context, dsn string, budget pgpool.Config) (*pgxpool.Pool, *pgxpool.Pool, error) {
	queryCfg, lockCfg, err := budget.CoreBudgets()
	if err != nil {
		return nil, nil, err
	}
	db, err := pgpool.Open(ctx, dsn, queryCfg, pgpool.PingTimeout)
	if err != nil {
		return nil, nil, err
	}
	lockDB, err := pgpool.Open(ctx, dsn, lockCfg, pgpool.PingTimeout)
	if err != nil {
		db.Close() // startup failed and its error is returned; the pool is never used
		return nil, nil, fmt.Errorf("lock pool: %w", err)
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
	// A process interruption is not a failed attempt, so resource operations
	// get theirs back; container creates keep it, as they always have.
	if recovered, err := resources.RecoverInterruptedOperations(ctx, operationCause, model.OperationRecoveryScope{ReturnAttempt: true}); err != nil {
		return fmt.Errorf("recover interrupted resource operations: %w", err)
	} else if len(recovered) > 0 {
		logger.Warn("requeued interrupted volume and image operations", "count", len(recovered))
	}
	if recovered, err := containers.RecoverInterruptedContainerCreates(ctx, operationCause, model.OperationRecoveryScope{}); err != nil {
		logger.Warn("failed to recover interrupted container create operations", "error", err)
	} else if len(recovered) > 0 {
		logger.Warn("requeued interrupted container create operations", "count", len(recovered))
	}
	if recovered, err := containers.FailInterruptedNonResumableOperations(ctx, operationCause, 0); err != nil {
		logger.Warn("failed to mark interrupted non-resumable operations", "error", err)
	} else if len(recovered) > 0 {
		logger.Warn("failed interrupted non-resumable operations", "count", len(recovered))
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

// warnWithoutTenantCgroupParent logs one warning when TENANT_CGROUP_PARENT is
// not set: tenant and build containers then sit under Docker's default
// parent, and nothing on the host caps the memory they take together. It
// never stops startup.
func warnWithoutTenantCgroupParent(logger *slog.Logger, parent string) {
	if strings.TrimSpace(parent) != "" {
		return
	}
	logger.Warn("TENANT_CGROUP_PARENT is not set: tenant and build containers run under Docker's default cgroup parent, "+
		"so no host cgroup caps the memory they take together and only the memory pressure worker and the kernel OOM killer protect the platform's services",
		"operation", "check_tenant_cgroup_parent")
}
