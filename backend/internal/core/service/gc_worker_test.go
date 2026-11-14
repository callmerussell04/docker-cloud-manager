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

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type gcRelease struct {
	at        time.Duration
	completed bool
}

// gcFixture drives GCWorker.Run inside a synctest bubble. The Registry GC gate
// answers each lease attempt with gate(at); the garbage-collect exec runs for
// gcRuns and then exits with gcExitCode. Every outcome call is recorded with
// its time since the fixture started.
type gcFixture struct {
	start time.Time
	cfg   config.SystemConfig
	logs  bytes.Buffer

	docker *coremocks.GCDockerAPI
	gate   *coremocks.GCRegistryGate
	builds *coremocks.GCBuildRepo
	images *coremocks.GCImageService

	mu       sync.Mutex
	answer   func(at time.Duration) model.RegistryGCAttempt
	renewed  func(at time.Duration) bool
	inspect  func(sinceStart time.Duration) (bool, int, error)
	startErr error

	attempts     []time.Duration
	renewals     []time.Duration
	releases     []gcRelease
	gcStarts     []time.Duration
	prunes       []time.Duration
	staleFetches []time.Duration
}

// The lease is the default budget, 15 minutes; the drain policy is one day.
const (
	gcTestLease      = 15 * time.Minute
	gcTestDrainAfter = 24 * time.Hour
)

func newGCFixture(t *testing.T) *gcFixture {
	cfg := coretest.SystemConfig()
	cfg.RegistryContainerName = "registry"
	cfg.GCWorkerIntervalMinutes = 60
	cfg.RegistryGCMaxDurationSeconds = int64(gcTestLease / time.Second)
	f := &gcFixture{
		start:   time.Now(),
		cfg:     cfg,
		docker:  coremocks.NewGCDockerAPI(t),
		gate:    coremocks.NewGCRegistryGate(t),
		builds:  coremocks.NewGCBuildRepo(t),
		images:  coremocks.NewGCImageService(t),
		answer:  func(time.Duration) model.RegistryGCAttempt { return model.RegistryGCAttempt{Acquired: true} },
		renewed: func(time.Duration) bool { return true },
	}
	f.runsFor(30*time.Second, 0)
	return f
}

// runsFor makes the exec run for d after it starts, then exit with exitCode.
func (f *gcFixture) runsFor(d time.Duration, exitCode int) {
	f.inspect = func(sinceStart time.Duration) (bool, int, error) {
		if sinceStart < d {
			return true, 0, nil
		}
		return false, exitCode, nil
	}
}

func (f *gcFixture) since() time.Duration { return time.Since(f.start) }

func (f *gcFixture) record(list *[]time.Duration) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	at := f.since()
	*list = append(*list, at)
	return at
}

func (f *gcFixture) expect() {
	lease := time.Duration(f.cfg.RegistryGCMaxDurationSeconds) * time.Second
	f.docker.EXPECT().PruneSystem(mock.Anything).RunAndReturn(func(context.Context) error {
		f.record(&f.prunes)
		return nil
	})
	f.builds.EXPECT().GetStaleBuilds(mock.Anything, mock.Anything).RunAndReturn(func(context.Context, time.Time) ([]model.Build, error) {
		f.record(&f.staleFetches)
		return nil, nil
	})
	f.gate.EXPECT().TryAcquireLease(mock.Anything, lease, gcTestDrainAfter).RunAndReturn(func(context.Context, time.Duration, time.Duration) (model.RegistryGCAttempt, error) {
		return f.answer(f.record(&f.attempts)), nil
	})
	f.gate.EXPECT().RenewLease(mock.Anything, lease).RunAndReturn(func(context.Context, time.Duration) (bool, error) {
		return f.renewed(f.record(&f.renewals)), nil
	}).Maybe()
	f.gate.EXPECT().ReleaseLease(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, completed bool) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.releases = append(f.releases, gcRelease{at: f.since(), completed: completed})
		return nil
	}).Maybe()
	f.docker.EXPECT().StartRegistryGarbageCollect(mock.Anything, "registry").RunAndReturn(func(context.Context, string) (string, error) {
		if f.startErr != nil {
			return "", f.startErr
		}
		f.record(&f.gcStarts)
		return "exec-id", nil
	}).Maybe()
	f.docker.EXPECT().InspectRegistryGarbageCollect(mock.Anything, "exec-id").RunAndReturn(func(context.Context, string) (bool, int, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.inspect(f.since() - f.gcStarts[len(f.gcStarts)-1])
	}).Maybe()
}

// run starts the worker and returns a stop function that cancels it and
// waits for Run to return.
func (f *gcFixture) run(t *testing.T) (stop func()) {
	t.Helper()
	f.expect()
	worker, err := service.NewGCWorker(service.GCWorkerDeps{
		Docker: f.docker, Images: f.images, Builds: f.builds, RegistryGC: f.gate,
		Config: coretest.StaticConfig{Config: f.cfg}, Logger: slog.New(slog.NewJSONHandler(&f.logs, nil)),
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()
	return func() {
		cancel()
		<-done
	}
}

// runFor runs the worker for d of bubble time.
func (f *gcFixture) runFor(t *testing.T, d time.Duration) {
	t.Helper()
	stop := f.run(t)
	time.Sleep(d)
	synctest.Wait()
	stop()
}

func (f *gcFixture) logged(t *testing.T, level string) []string {
	t.Helper()
	var messages []string
	for _, entry := range decodeLogs(t, &f.logs) {
		if entry["level"] == level {
			messages = append(messages, entry["msg"].(string))
		}
	}
	return messages
}

func TestNewGCWorkerRejectsMissingDependencies(t *testing.T) {
	valid := func(t *testing.T) service.GCWorkerDeps {
		return service.GCWorkerDeps{
			Docker: coremocks.NewGCDockerAPI(t), Images: coremocks.NewGCImageService(t), Builds: coremocks.NewGCBuildRepo(t),
			RegistryGC: coremocks.NewGCRegistryGate(t), Config: coretest.StaticConfig{},
		}
	}
	depstest.RequireRejectsMissing(t, valid, service.NewGCWorker, map[string]string{
		"Docker": "docker", "Images": "images", "Builds": "builds", "RegistryGC": "registry gc", "Config": "config",
	})
}

// While a build runs, Registry GC is skipped and retried every minute; once
// the lease is taken it runs, and its observed end releases the lease as a
// completed collection. An overdue collection drains first, then runs the same
// way.
func TestGCWorkerRetriesRegistryGCEveryMinuteUntilNoBuildRuns(t *testing.T) {
	tests := []struct {
		name    string
		waiting model.RegistryGCAttempt
	}{
		{name: "a build runs", waiting: model.RegistryGCAttempt{}},
		{name: "overdue collection drains", waiting: model.RegistryGCAttempt{Draining: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newGCFixture(t)
				f.answer = func(at time.Duration) model.RegistryGCAttempt {
					if at < 3*time.Minute {
						return tt.waiting
					}
					return model.RegistryGCAttempt{Acquired: true, Draining: tt.waiting.Draining}
				}
				f.runFor(t, 10*time.Minute)

				require.Equal(t, []time.Duration{0, time.Minute, 2 * time.Minute, 3 * time.Minute}, f.attempts)
				require.Equal(t, []time.Duration{3 * time.Minute}, f.gcStarts)
				require.Equal(t, []gcRelease{{at: 3*time.Minute + 30*time.Second, completed: true}}, f.releases)
				require.Empty(t, f.renewals)
				require.Empty(t, f.logged(t, "ERROR"))
			})
		})
	}
}

// Docker pruning and the stale-build pass run every interval although Registry
// GC never gets its lease.
func TestGCWorkerKeepsPruningWhileRegistryGCIsDeferred(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newGCFixture(t)
		f.answer = func(time.Duration) model.RegistryGCAttempt { return model.RegistryGCAttempt{} }
		f.runFor(t, 150*time.Minute)

		every := []time.Duration{0, 60 * time.Minute, 120 * time.Minute}
		require.Equal(t, every, f.prunes)
		require.Equal(t, every, f.staleFetches)
		require.Empty(t, f.gcStarts)
		require.Greater(t, len(f.attempts), 100, "GC keeps retrying every minute")
	})
}

// A collection that runs past its budget keeps the lease: it is renewed before
// every deadline, one error is logged, and the observed end releases it. An
// exec that cannot be inspected for a while is presumed running and keeps its
// lease too.
func TestGCWorkerRenewsTheLeaseOfAnOverrunningCollection(t *testing.T) {
	tests := []struct {
		name          string
		inspectFailAt time.Duration // inspection fails from here for 5 minutes (0: never)
	}{
		{name: "observed throughout"},
		{name: "inspection fails for a while", inspectFailAt: 12 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newGCFixture(t)
				f.cfg.RegistryGCMaxDurationSeconds = 600
				f.runsFor(25*time.Minute, 0)
				if tt.inspectFailAt > 0 {
					observed := f.inspect
					f.inspect = func(sinceStart time.Duration) (bool, int, error) {
						if sinceStart >= tt.inspectFailAt && sinceStart < tt.inspectFailAt+5*time.Minute {
							return false, 0, errors.New("docker unavailable")
						}
						return observed(sinceStart)
					}
				}
				f.runFor(t, 30*time.Minute)

				require.Equal(t, []gcRelease{{at: 25 * time.Minute, completed: true}}, f.releases)
				require.NotEmpty(t, f.renewals)
				leaseEnds := 10 * time.Minute
				for _, renewal := range f.renewals {
					require.Less(t, renewal, leaseEnds, "renewed before the lease expired")
					leaseEnds = renewal + 10*time.Minute
				}
				require.Greater(t, leaseEnds, 25*time.Minute, "the lease covers the whole collection")
				require.Equal(t, []string{"registry garbage collection runs past its time budget; builds keep waiting"}, f.logged(t, "ERROR"))
			})
		})
	}
}

// A lease that lapsed before its renewal is not renewed over a build that may
// have started since: the worker stops tracking the collection, leaves the
// lease alone, logs an error and retries every minute.
func TestGCWorkerStopsTrackingACollectionWhoseLeaseLapsed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newGCFixture(t)
		f.cfg.RegistryGCMaxDurationSeconds = 600
		f.runsFor(time.Hour, 0)
		f.renewed = func(time.Duration) bool { return false }
		f.answer = func(at time.Duration) model.RegistryGCAttempt { return model.RegistryGCAttempt{Acquired: at == 0} }
		f.runFor(t, 12*time.Minute)

		require.Equal(t, []time.Duration{9 * time.Minute}, f.renewals)
		require.Empty(t, f.releases)
		require.Equal(t, []time.Duration{0, 10 * time.Minute, 11 * time.Minute, 12 * time.Minute}, f.attempts)
		require.Equal(t, []string{"registry garbage collection lease lapsed before it was renewed"}, f.logged(t, "ERROR"))
	})
}

// Shutdown ends the wait for a running collection at once and leaves the lease
// to expire at its deadline.
func TestGCWorkerStopsWaitingForRegistryGCOnCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newGCFixture(t)
		f.runsFor(time.Hour, 0)
		stop := f.run(t)
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		stopped := time.Now()
		stop()

		require.Equal(t, stopped, time.Now(), "Run returns without waiting for the process")
		require.Equal(t, []time.Duration{0}, f.gcStarts)
		require.Empty(t, f.releases)
	})
}

// A collection that cannot start, or whose exec is gone, releases the lease
// without recording a completion; one that exits non-zero was observed
// finished and records it. Each failure is logged as an error, and the next
// try waits for the next interval.
func TestGCWorkerReleasesTheLeaseWhenCollectionFails(t *testing.T) {
	gone := func(time.Duration) (bool, int, error) {
		return false, 0, apperrors.Wrap(apperrors.ErrNotFound, apperrors.ErrNotFound.Error(), errors.New("no such exec"))
	}
	tests := []struct {
		name     string
		startErr error
		exitCode int
		inspect  func(time.Duration) (bool, int, error)
		want     gcRelease
	}{
		{name: "exec cannot start", startErr: errors.New("registry container is gone"), want: gcRelease{at: 0, completed: false}},
		{name: "exec is gone", inspect: gone, want: gcRelease{at: 0, completed: false}},
		{name: "process exits non-zero", exitCode: 1, want: gcRelease{at: 30 * time.Second, completed: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newGCFixture(t)
				f.startErr = tt.startErr
				f.runsFor(30*time.Second, tt.exitCode)
				if tt.inspect != nil {
					f.inspect = tt.inspect
				}
				f.runFor(t, 10*time.Minute)

				require.Equal(t, []gcRelease{tt.want}, f.releases)
				require.Equal(t, []time.Duration{0}, f.attempts)
				require.Equal(t, []string{"registry garbage collection failed"}, f.logged(t, "ERROR"))
			})
		})
	}
}
