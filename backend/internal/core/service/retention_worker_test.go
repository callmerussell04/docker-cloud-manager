package service_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/stretchr/testify/require"
)

// retentionStoreFake holds how many expired rows of each kind remain and
// deletes up to limit of them per call, recording each cutoff's age.
type retentionStoreFake struct {
	mu                               sync.Mutex
	operations, credentials, objects int64
	operationsErr                    error
	cutoffAges                       []time.Duration
	calls                            int
}

func (f *retentionStoreFake) take(remaining *int64, cutoff time.Time, limit int) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.cutoffAges = append(f.cutoffAges, time.Since(cutoff))
	n := min(*remaining, int64(limit))
	*remaining -= n
	return n
}

func (f *retentionStoreFake) DeleteFinishedOperations(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	if f.operationsErr != nil {
		return 0, f.operationsErr
	}
	return f.take(&f.operations, cutoff, limit), nil
}

func (f *retentionStoreFake) DeleteSpentBuildCredentials(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	return f.take(&f.credentials, cutoff, limit), nil
}

func (f *retentionStoreFake) DeleteReleasedStagedObjects(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	return f.take(&f.objects, cutoff, limit), nil
}

func (f *retentionStoreFake) remaining() (int64, int64, int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.operations, f.credentials, f.objects
}

func runRetentionWorker(t *testing.T, store *retentionStoreFake, logs *bytes.Buffer) {
	t.Helper()
	worker := service.NewRetentionWorker(store, slog.New(slog.NewTextHandler(logs, nil)))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func TestRetentionWorkerDeletesEverythingExpiredSoonAfterStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// More than two batches of operations, so the worker must keep deleting.
		store := &retentionStoreFake{operations: 2500, credentials: 3, objects: 1}
		var logs bytes.Buffer
		runRetentionWorker(t, store, &logs)

		time.Sleep(2 * time.Minute)
		synctest.Wait()

		operations, credentials, objects := store.remaining()
		require.Zero(t, operations)
		require.Zero(t, credentials)
		require.Zero(t, objects)
		for _, age := range store.cutoffAges {
			require.Equal(t, 7*24*time.Hour, age, "rows are kept for seven days")
		}
		require.Contains(t, logs.String(), "operations=2500")
		require.Contains(t, logs.String(), "build_credentials=3")
		require.Contains(t, logs.String(), "staged_objects=1")
	})
}

func TestRetentionWorkerRunsAgainEveryHour(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &retentionStoreFake{}
		var logs bytes.Buffer
		runRetentionWorker(t, store, &logs)
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		store.mu.Lock()
		store.objects = 5
		store.mu.Unlock()

		time.Sleep(time.Hour)
		synctest.Wait()

		_, _, objects := store.remaining()
		require.Zero(t, objects)
	})
}

func TestRetentionWorkerStaysQuietWhenNothingExpired(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &retentionStoreFake{}
		var logs bytes.Buffer
		runRetentionWorker(t, store, &logs)

		time.Sleep(2 * time.Minute)
		synctest.Wait()

		require.Empty(t, logs.String())
	})
}

func TestRetentionWorkerKeepsGoingWhenOneDeleteFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &retentionStoreFake{operationsErr: errors.New("connection reset"), credentials: 2, objects: 2}
		var logs bytes.Buffer
		runRetentionWorker(t, store, &logs)

		time.Sleep(2 * time.Minute)
		synctest.Wait()

		_, credentials, objects := store.remaining()
		require.Zero(t, credentials)
		require.Zero(t, objects)
		require.Contains(t, logs.String(), "level=ERROR")
		require.Contains(t, logs.String(), "connection reset")
	})
}
