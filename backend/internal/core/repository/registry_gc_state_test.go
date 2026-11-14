package repository_test

import (
	"context"
	"crypto/sha256"
	"sync"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const gcDrainAfter = 24 * time.Hour

type gcGateFixture struct {
	t      *testing.T
	ctx    context.Context
	db     *pgxpool.Pool
	builds *repository.BuildRepository
	grants *repository.RegistryGrantRepository
	gc     *repository.RegistryGCRepository
	owner  uuid.UUID
}

func newGCGateFixture(t *testing.T) *gcGateFixture {
	db := dbtest.OpenCorePostgres(t)
	return &gcGateFixture{
		t:      t,
		ctx:    context.Background(),
		db:     db,
		builds: repository.NewBuildRepository(db, db),
		grants: repository.NewRegistryGrantRepository(db, db),
		gc:     repository.NewRegistryGCRepository(db),
		owner:  uuid.New(),
	}
}

func (f *gcGateFixture) queue() uuid.UUID {
	imageID, buildID := uuid.New(), uuid.New()
	require.NoError(f.t, f.builds.CreateQueuedBuild(f.ctx, model.Image{ID: imageID, RegistryTag: imageID.String(), OwnerID: f.owner, Tag: "gc-" + buildID.String() + ":latest"}, model.Build{ID: buildID, ImageID: imageID, OwnerID: f.owner, Status: model.BuildStatusPending}, model.BuildQueueOutbox{ID: uuid.New(), BuildID: buildID, Exchange: "test", RoutingKey: "test", Payload: []byte(`{}`)}))
	return buildID
}

func (f *gcGateFixture) claim(id uuid.UUID) bool {
	hash := sha256.Sum256([]byte(id.String()))
	b, started, err := f.grants.ClaimBuild(f.ctx, id, hash[:], time.Minute, 10)
	require.NoError(f.t, err)
	if !started {
		require.Equal(f.t, model.BuildStatusPending, b.Status, "a refused claim answers unclaimed and pending")
	}
	return started
}

func (f *gcGateFixture) finishBuild(id uuid.UUID) {
	_, err := f.db.Exec(f.ctx, `UPDATE builds SET status='success', finished_at=clock_timestamp() WHERE id=$1`, id)
	require.NoError(f.t, err)
}

func (f *gcGateFixture) acquire() model.RegistryGCAttempt {
	attempt, err := f.gc.TryAcquireLease(f.ctx, time.Minute, gcDrainAfter)
	require.NoError(f.t, err)
	return attempt
}

// A live lease refuses every claim until it is released; once released, the
// same build is claimed.
func TestRegistryGCLeaseRefusesBuildClaims(t *testing.T) {
	f := newGCGateFixture(t)
	build := f.queue()

	require.Equal(t, model.RegistryGCAttempt{Acquired: true}, f.acquire())
	require.False(t, f.claim(build), "no build starts while Registry GC holds the lease")

	require.NoError(t, f.gc.ReleaseLease(f.ctx, true))
	require.True(t, f.claim(build))
}

// An expired lease no longer refuses claims and cannot be renewed; a live
// lease is renewed and keeps refusing them.
func TestRegistryGCLeaseExpiresAndRenews(t *testing.T) {
	f := newGCGateFixture(t)
	require.True(t, f.acquire().Acquired)
	_, err := f.db.Exec(f.ctx, `UPDATE registry_gc_state SET lease_until=clock_timestamp()-interval '1 second'`)
	require.NoError(t, err)
	require.True(t, f.claim(f.queue()), "an expired lease refuses nothing")

	renewed, err := f.gc.RenewLease(f.ctx, time.Minute)
	require.NoError(t, err)
	require.False(t, renewed, "a lapsed lease is not renewed over a build claimed since")
	require.True(t, f.claim(f.queue()))

	_, err = f.db.Exec(f.ctx, `UPDATE builds SET status='success' WHERE status='running'`)
	require.NoError(t, err)
	require.True(t, f.acquire().Acquired)
	renewed, err = f.gc.RenewLease(f.ctx, time.Minute)
	require.NoError(t, err)
	require.True(t, renewed)
	require.False(t, f.claim(f.queue()), "a renewed lease refuses claims")
}

// The lease is never taken while a build runs. A collection overdue by the
// drain policy sets draining instead: new claims are refused, the running
// build finishes, and then the lease is taken. Releasing it clears draining.
func TestRegistryGCLeaseWaitsForRunningBuildsAndDrains(t *testing.T) {
	f := newGCGateFixture(t)
	running := f.queue()
	require.True(t, f.claim(running))

	require.Equal(t, model.RegistryGCAttempt{}, f.acquire(), "a running build keeps the lease free")
	require.True(t, f.claim(f.queue()), "a recent collection does not drain")

	_, err := f.db.Exec(f.ctx, `UPDATE registry_gc_state SET last_completed_at=clock_timestamp()-interval '25 hours'`)
	require.NoError(t, err)
	require.Equal(t, model.RegistryGCAttempt{Draining: true}, f.acquire())
	waiting := f.queue()
	require.False(t, f.claim(waiting), "draining refuses new claims")
	require.Equal(t, model.RegistryGCAttempt{Draining: true}, f.acquire(), "builds still run")

	_, err = f.db.Exec(f.ctx, `UPDATE builds SET status='success', finished_at=clock_timestamp() WHERE status='running'`)
	require.NoError(t, err)
	require.Equal(t, model.RegistryGCAttempt{Acquired: true, Draining: true}, f.acquire())

	require.NoError(t, f.gc.ReleaseLease(f.ctx, false))
	require.True(t, f.claim(waiting), "a failed collection clears draining too")
	f.finishBuild(waiting)
	require.Equal(t, model.RegistryGCAttempt{Draining: true}, f.acquireAfterRunning(t), "a failed collection is not a completed one")
}

// acquireAfterRunning claims a fresh build and tries the lease while it runs.
func (f *gcGateFixture) acquireAfterRunning(t *testing.T) model.RegistryGCAttempt {
	t.Helper()
	require.True(t, f.claim(f.queue()))
	return f.acquire()
}

// A completed collection resets the drain clock.
func TestRegistryGCCompletedCollectionResetsDrainClock(t *testing.T) {
	f := newGCGateFixture(t)
	_, err := f.db.Exec(f.ctx, `UPDATE registry_gc_state SET last_completed_at=clock_timestamp()-interval '25 hours'`)
	require.NoError(t, err)
	require.True(t, f.acquire().Acquired)
	require.NoError(t, f.gc.ReleaseLease(f.ctx, true))

	require.Equal(t, model.RegistryGCAttempt{}, f.acquireAfterRunning(t))
}

// The lease and a claim race under the build claim lock: in no round does a
// build start while the lease is held.
func TestRegistryGCLeaseAndConcurrentClaimNeverBothSucceed(t *testing.T) {
	f := newGCGateFixture(t)
	for attempt := range 20 {
		build := f.queue()
		start := make(chan struct{})
		var wg sync.WaitGroup
		var acquired, started bool
		var acquireErr, claimErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			var a model.RegistryGCAttempt
			a, acquireErr = f.gc.TryAcquireLease(f.ctx, time.Minute, gcDrainAfter)
			acquired = a.Acquired
		}()
		go func() {
			defer wg.Done()
			<-start
			hash := sha256.Sum256([]byte(build.String()))
			_, started, claimErr = f.grants.ClaimBuild(f.ctx, build, hash[:], time.Minute, 10)
		}()
		close(start)
		wg.Wait()
		require.NoError(t, acquireErr)
		require.NoError(t, claimErr)
		require.False(t, acquired && started, "attempt %d: a build started under the GC lease", attempt)
		require.True(t, acquired || started, "attempt %d", attempt)

		if acquired {
			require.NoError(t, f.gc.ReleaseLease(f.ctx, true))
		}
		f.finishBuild(build)
	}
}
