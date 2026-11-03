package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestAdmissionPoolPreservesQueryProgress(t *testing.T) {
	db, locks := openCoreBudgetPools(t, 2)
	builds := repository.NewBuildRepository(db, locks)
	containers := repository.NewContainerRepository(db, locks)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := containers.AcquireOwnerCapacityLock(ctx, uuid.New())
	require.NoError(t, err)
	defer release()
	waiterCtx, cancelWaiters := context.WithCancel(ctx)
	defer cancelWaiters()
	results := make(chan error, 9)
	for range 8 {
		go func() {
			unlock, err := builds.AcquireCapacityLock(waiterCtx)
			if err == nil {
				unlock()
			}
			results <- err
		}()
	}
	go func() {
		results <- containers.SaveWithMountsAndOperation(waiterCtx, model.Container{OwnerID: uuid.New()}, nil, model.ResourceOperation{}, true, true)
	}()
	require.Eventually(t, func() bool { return dbtest.PendingAcquires(locks) >= 9 }, time.Second, time.Millisecond)
	count, err := builds.CountActive(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	require.LessOrEqual(t, int(db.Stat().TotalConns())+int(locks.Stat().TotalConns()), 2)
	cancelWaiters()
	for range 9 {
		require.ErrorIs(t, <-results, context.Canceled)
	}
	release()
	unlock, err := builds.AcquireCapacityLock(ctx)
	require.NoError(t, err)
	unlock()
}

func TestAdmissionLockCancellationAndContention(t *testing.T) {
	db := dbtest.OpenCorePostgres(t)
	locks := dbtest.OpenCorePeerPool(t, db, dbtest.Budget(1))
	peer := dbtest.OpenCorePeerPool(t, db, dbtest.Budget(1))
	holder := repository.NewBuildRepository(db, peer)
	waiter := repository.NewContainerRepository(db, locks)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := holder.AcquireCapacityLock(ctx)
	require.NoError(t, err)
	defer release()
	waitCtx, cancelWait := context.WithCancel(ctx)
	defer cancelWait()
	result := make(chan error, 1)
	go func() {
		unlock, err := waiter.AcquireOwnerCapacityLock(waitCtx, uuid.New())
		if err == nil {
			unlock()
		}
		result <- err
	}()
	require.Eventually(t, func() bool {
		var waiting int
		err := db.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted`).Scan(&waiting)
		return err == nil && waiting > 0
	}, time.Second, time.Millisecond)
	cancelWait()
	require.ErrorIs(t, <-result, context.Canceled)
	release()
	unlock, err := waiter.AcquireOwnerCapacityLock(ctx, uuid.New())
	require.NoError(t, err)
	unlock()
	holdCtx, cancelHold := context.WithCancel(ctx)
	defer cancelHold()
	unlock, err = holder.AcquireCapacityLock(holdCtx)
	require.NoError(t, err)
	cancelHold()
	next, err := waiter.AcquireOwnerCapacityLock(ctx, uuid.New())
	require.NoError(t, err)
	next()
	unlock()
}

func TestCoreQueriesStayBoundedAndCancel(t *testing.T) {
	db, peer := openCoreBudgetPools(t, 2)
	repo := repository.NewBuildRepository(db, peer)
	dbtest.CheckPoolProgress(t, db, peer, "builds", func(ctx context.Context) error { _, err := repo.CountActive(ctx); return err })
}

func TestRegistryLockPreservesQueryProgress(t *testing.T) {
	db, locks := openCoreBudgetPools(t, 2)
	grants := repository.NewRegistryGrantRepository(db, locks)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlock, err := grants.LockRegistryRepository(ctx, "pool-test-repository")
	require.NoError(t, err)
	defer unlock()

	results := make(chan error, 8)
	for range 8 {
		go func() {
			release, err := grants.LockRegistryRepository(ctx, "pool-test-repository")
			if err == nil {
				release()
			}
			results <- err
		}()
	}
	require.Eventually(t, func() bool { return dbtest.PendingAcquires(locks) >= 8 }, time.Second, time.Millisecond)
	facts, err := grants.RegistryCleanupFacts(ctx, model.RegistryCleanup{Repository: "pool-test-repository", Tag: "latest"}, service.RegistryCleanupDelay)
	require.NoError(t, err)
	require.Equal(t, model.RegistryCleanupFacts{}, facts)
	unlock()
	for range 8 {
		require.NoError(t, <-results)
	}
	require.LessOrEqual(t, int(db.Stat().TotalConns())+int(locks.Stat().TotalConns()), 2)
}

// openCoreBudgetPools opens Core's query and lock pools with the split budget
// production derives from maxConns, in one isolated schema.
func openCoreBudgetPools(t *testing.T, maxConns int) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	base := dbtest.OpenCorePostgres(t)
	query, locks, err := dbtest.Budget(maxConns).CoreBudgets()
	require.NoError(t, err)
	return dbtest.OpenCorePeerPool(t, base, query), dbtest.OpenCorePeerPool(t, base, locks)
}
