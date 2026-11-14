package repository_test

import (
	"context"
	"crypto/sha256"
	"sync"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRegistryGrantClaimIsExclusiveAndRevocationSurvivesRestart(t *testing.T) {
	db := dbtest.OpenCorePostgres(t)
	ctx := context.Background()
	builds := repository.NewBuildRepository(db, db)
	grants := repository.NewRegistryGrantRepository(db, db)
	owner, imageID, buildID := uuid.New(), uuid.New(), uuid.New()
	require.NoError(t, builds.CreateQueuedBuild(ctx, model.Image{ID: imageID, RegistryTag: imageID.String(), OwnerID: owner, Tag: "destination:latest"}, model.Build{ID: buildID, ImageID: imageID, OwnerID: owner, Status: model.BuildStatusPending, StartedAt: time.Now()}, model.BuildQueueOutbox{ID: uuid.New(), BuildID: buildID, Exchange: "test", RoutingKey: "test", Payload: []byte(`{}`)}))
	hashes := [][32]byte{sha256.Sum256([]byte("worker-one-secret")), sha256.Sum256([]byte("worker-two-secret"))}
	type result struct {
		worker  int
		started bool
		err     error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for i := range hashes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, started, err := grants.ClaimBuild(ctx, buildID, hashes[i][:], time.Minute, 10)
			results <- result{i, started, err}
		}(i)
	}
	wg.Wait()
	close(results)
	winner := -1
	for result := range results {
		require.NoError(t, result.err)
		if result.started {
			require.Equal(t, -1, winner)
			winner = result.worker
		}
	}
	require.NotEqual(t, -1, winner)
	principal, err := grants.AuthorizeCredential(ctx, hashes[winner][:], time.Now())
	require.NoError(t, err)
	require.Equal(t, owner, principal.OwnerID)
	require.Equal(t, owner.String()+"_destination", principal.Repository)
	_, err = grants.AuthorizeCredential(ctx, hashes[1-winner][:], time.Now())
	require.ErrorIs(t, err, apperrors.ErrForbidden)
	_, err = grants.AuthorizeCredential(ctx, hashes[winner][:], time.Now().Add(2*time.Minute))
	require.ErrorIs(t, err, apperrors.ErrForbidden)
	require.NoError(t, grants.RevokeBuild(ctx, buildID, model.RegistryTagDelete, service.RegistryCleanupDelay))
	restarted := repository.NewRegistryGrantRepository(db, db)
	_, err = restarted.AuthorizeCredential(ctx, hashes[winner][:], time.Now())
	require.ErrorIs(t, err, apperrors.ErrForbidden)
	// Deletion fences only after the repository's builds stop running.
	_, err = db.Exec(ctx, `UPDATE builds SET status=$2 WHERE id=$1`, buildID, model.BuildStatusCanceled)
	require.NoError(t, err)
	remaining, err := restarted.FenceRepository(ctx, principal.Repository, service.RegistryCleanupDelay)
	require.NoError(t, err)
	require.Greater(t, remaining, 179*time.Second, "cleanup must wait for token skew and admitted writes")
	require.NoError(t, builds.Delete(ctx, buildID))
	require.NoError(t, repository.NewImageRepository(db).Delete(ctx, imageID))
	exists, err := restarted.RepositoryExists(ctx, principal.Repository)
	require.NoError(t, err)
	require.True(t, exists, "durable cleanup authority must outlive resource rows")
	jobs, err := restarted.PendingRegistryCleanup(ctx)
	require.NoError(t, err)
	require.Empty(t, jobs, "cleanup cannot run before the fence")
}

func TestRegistryCleanupCannotRaceReplacementClaim(t *testing.T) {
	db := dbtest.OpenCorePostgres(t)
	ctx := context.Background()
	builds := repository.NewBuildRepository(db, db)
	grants := repository.NewRegistryGrantRepository(db, db)
	owner := uuid.New()
	queue := func(tag string) uuid.UUID {
		imageID, buildID := uuid.New(), uuid.New()
		require.NoError(t, builds.CreateQueuedBuild(ctx, model.Image{ID: imageID, RegistryTag: imageID.String(), OwnerID: owner, Tag: tag}, model.Build{ID: buildID, ImageID: imageID, OwnerID: owner, Status: model.BuildStatusPending, StartedAt: time.Now()}, model.BuildQueueOutbox{ID: uuid.New(), BuildID: buildID, Exchange: "test", RoutingKey: "test", Payload: []byte(`{}`)}))
		return buildID
	}
	oldID := queue("replacement:latest")
	oldHash := sha256.Sum256([]byte("old-secret"))
	old, started, err := grants.ClaimBuild(ctx, oldID, oldHash[:], time.Minute, 10)
	require.NoError(t, err)
	require.True(t, started)
	require.NoError(t, grants.RevokeBuild(ctx, oldID, model.RegistryTagDelete, service.RegistryCleanupDelay))
	require.NoError(t, repository.NewImageRepository(db).MarkBuildFailedAndDeleteImageTx(ctx, oldID, old.ImageID, model.BuildStatusCanceled))
	_, err = db.Exec(ctx, `UPDATE registry_build_credentials SET last_issued_at=clock_timestamp()-interval '5 minutes' WHERE build_id=$1`, oldID)
	require.NoError(t, err)
	repo := owner.String() + "_replacement"
	job := model.RegistryCleanup{ID: oldID, Repository: repo, Tag: "latest"}
	replacementID := queue("replacement:v2")
	unlock, err := grants.LockRegistryRepository(ctx, repo)
	require.NoError(t, err)
	nextHash := sha256.Sum256([]byte("replacement-secret"))
	_, started, err = grants.ClaimBuild(ctx, replacementID, nextHash[:], time.Minute, 10)
	require.ErrorIs(t, err, apperrors.ErrUnavailable)
	require.False(t, started)
	unlock()
	_, started, err = grants.ClaimBuild(ctx, replacementID, nextHash[:], time.Minute, 10)
	require.NoError(t, err)
	require.True(t, started)
	facts, err := grants.RegistryCleanupFacts(ctx, job, service.RegistryCleanupDelay)
	require.NoError(t, err)
	require.True(t, facts.PublisherActive, "repository-wide grants must keep sibling-tag publishers ahead of cleanup")
	// An unlocked deletion fence can race a claim; it must not revoke a running publisher.
	_, err = grants.FenceRepository(ctx, repo, service.RegistryCleanupDelay)
	require.NoError(t, err)
	_, err = grants.AuthorizeCredential(ctx, nextHash[:], time.Now())
	require.NoError(t, err, "fencing must leave a running build's credential usable")
}

// Builders on any number of instances share one cap: a claim is refused while
// maxRunning builds already run, and the refused build stays pending.
func TestRegistryGrantClaimCapsRunningBuilds(t *testing.T) {
	db := dbtest.OpenCorePostgres(t)
	ctx := context.Background()
	builds := repository.NewBuildRepository(db, db)
	grants := repository.NewRegistryGrantRepository(db, db)
	owner := uuid.New()
	queue := func() uuid.UUID {
		imageID, buildID := uuid.New(), uuid.New()
		require.NoError(t, builds.CreateQueuedBuild(ctx, model.Image{ID: imageID, RegistryTag: imageID.String(), OwnerID: owner, Tag: "capped-" + buildID.String() + ":latest"}, model.Build{ID: buildID, ImageID: imageID, OwnerID: owner, Status: model.BuildStatusPending}, model.BuildQueueOutbox{ID: uuid.New(), BuildID: buildID, Exchange: "test", RoutingKey: "test", Payload: []byte(`{}`)}))
		return buildID
	}
	claim := func(id uuid.UUID, maxRunning int) (model.Build, bool, error) {
		hash := sha256.Sum256([]byte(id.String()))
		return grants.ClaimBuild(ctx, id, hash[:], time.Minute, maxRunning)
	}
	for range 2 {
		_, started, err := claim(queue(), 2)
		require.NoError(t, err)
		require.True(t, started)
	}

	refused := queue()
	b, started, err := claim(refused, 2)
	require.NoError(t, err)
	require.False(t, started, "no slot is free with max_concurrent_builds running")
	require.Equal(t, model.BuildStatusPending, b.Status)
	persisted, err := builds.GetByID(ctx, refused)
	require.NoError(t, err)
	require.Equal(t, model.BuildStatusPending, persisted.Status)
	require.True(t, persisted.StartedAt.IsZero())
	var credentials int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM registry_build_credentials WHERE build_id=$1`, refused).Scan(&credentials))
	require.Zero(t, credentials, "a refused claim issues no credential")

	_, started, err = claim(refused, 3)
	require.NoError(t, err)
	require.True(t, started, "one free slot admits the claim")
}

func TestRegistryGrantConcurrentClaimsTakeTheLastSlotOnce(t *testing.T) {
	db := dbtest.OpenCorePostgres(t)
	ctx := context.Background()
	builds := repository.NewBuildRepository(db, db)
	grants := repository.NewRegistryGrantRepository(db, db)
	owner := uuid.New()
	ids := make([]uuid.UUID, 2)
	for i := range ids {
		imageID, buildID := uuid.New(), uuid.New()
		require.NoError(t, builds.CreateQueuedBuild(ctx, model.Image{ID: imageID, RegistryTag: imageID.String(), OwnerID: owner, Tag: "race-" + buildID.String() + ":latest"}, model.Build{ID: buildID, ImageID: imageID, OwnerID: owner, Status: model.BuildStatusPending}, model.BuildQueueOutbox{ID: uuid.New(), BuildID: buildID, Exchange: "test", RoutingKey: "test", Payload: []byte(`{}`)}))
		ids[i] = buildID
	}
	for attempt := range 20 {
		_, err := db.Exec(ctx, `UPDATE builds SET status='pending', started_at=NULL WHERE id = ANY($1)`, ids)
		require.NoError(t, err)
		_, err = db.Exec(ctx, `DELETE FROM registry_build_credentials WHERE build_id = ANY($1)`, ids)
		require.NoError(t, err)

		start := make(chan struct{})
		errs := make(chan error, len(ids))
		var wg sync.WaitGroup
		for _, id := range ids {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				hash := sha256.Sum256([]byte(id.String()))
				_, _, err := grants.ClaimBuild(ctx, id, hash[:], time.Minute, 1)
				errs <- err
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		var running int
		require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM builds WHERE status='running' AND id = ANY($1)`, ids).Scan(&running))
		require.Equal(t, 1, running, "attempt %d", attempt)
	}
}

// A claim pushes to the image row's own Registry tag, and cleanup treats a tag
// as available only when an available row's Registry tag is that tag: a
// rebuild's tag is never shielded by the image available under the same user
// tag, and an available image built before migration 0019 keeps its version tag.
func TestRegistryGrantClaimAndCleanupFactsUseTheRowsRegistryTag(t *testing.T) {
	db := dbtest.OpenCorePostgres(t)
	ctx := context.Background()
	builds := repository.NewBuildRepository(db, db)
	images := repository.NewImageRepository(db)
	grants := repository.NewRegistryGrantRepository(db, db)
	owner := uuid.New()
	repo := owner.String() + "_app"
	legacyID := uuid.New()
	require.NoError(t, images.Save(ctx, model.Image{ID: legacyID, OwnerID: owner, Tag: "app:latest", RegistryTag: "latest", SizeMB: 1, Status: model.ImageStatusAvailable}))

	imageID, buildID := uuid.New(), uuid.New()
	require.NoError(t, builds.CreateQueuedBuild(ctx, model.Image{ID: imageID, RegistryTag: imageID.String(), OwnerID: owner, Tag: "app:latest"}, model.Build{ID: buildID, ImageID: imageID, OwnerID: owner, Status: model.BuildStatusPending}, model.BuildQueueOutbox{ID: uuid.New(), BuildID: buildID, Exchange: "test", RoutingKey: "test", Payload: []byte(`{}`)}))
	hash := sha256.Sum256([]byte("rebuild-secret"))
	claimed, started, err := grants.ClaimBuild(ctx, buildID, hash[:], time.Minute, 10)
	require.NoError(t, err)
	require.True(t, started)
	require.Equal(t, repo+":"+imageID.String(), claimed.RegistryCredential.Destination)

	require.NoError(t, grants.RevokeBuild(ctx, buildID, model.RegistryTagDelete, service.RegistryCleanupDelay))
	require.NoError(t, images.MarkBuildFailedAndDeleteImageTx(ctx, buildID, imageID, model.BuildStatusFailedQuotaExceeded))
	_, err = db.Exec(ctx, `UPDATE registry_build_credentials SET last_issued_at=clock_timestamp()-interval '1 hour'`)
	require.NoError(t, err)

	rebuildFacts, err := grants.RegistryCleanupFacts(ctx, model.RegistryCleanup{ID: buildID, Repository: repo, Tag: imageID.String()}, service.RegistryCleanupDelay)
	require.NoError(t, err)
	require.Equal(t, model.RegistryCleanupFacts{}, rebuildFacts, "the rejected rebuild's own tag is deleted")
	legacyFacts, err := grants.RegistryCleanupFacts(ctx, model.RegistryCleanup{ID: uuid.New(), Repository: repo, Tag: "latest"}, service.RegistryCleanupDelay)
	require.NoError(t, err)
	require.True(t, legacyFacts.TagAvailable, "the available image's Registry tag is kept")
}
