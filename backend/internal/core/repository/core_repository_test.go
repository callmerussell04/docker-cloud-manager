package repository_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestCoreMigrationsApply(t *testing.T) {
	db := dbtest.OpenCorePostgres(t)

	require.GreaterOrEqual(t, dbtest.CountRows(t, db, "projects"), 0)
}

func TestBuildRepositoryCreateQueuedBuildPersistsTransaction(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewBuildRepository(db, db)

	ownerID := uuid.New()
	imageID := uuid.New()
	buildID := uuid.New()
	outboxID := uuid.New()
	payload, err := json.Marshal(buildqueue.ImageBuildMessage{
		BuildID:   buildID.String(),
		ImageID:   imageID.String(),
		OwnerID:   ownerID.String(),
		Tag:       "repo/demo:latest",
		CreatedAt: time.Now().Unix(),
	})
	require.NoError(t, err)

	err = repo.CreateQueuedBuild(ctx,
		model.Image{ID: imageID, RegistryTag: imageID.String(), OwnerID: ownerID, Tag: "repo/demo:latest"},
		model.Build{
			ID:               buildID,
			ImageID:          imageID,
			OwnerID:          ownerID,
			Status:           model.BuildStatusPending,
			LogObjectKey:     "build-logs/demo.log",
			ArchiveObjectKey: "build-archives/demo.zip",
			StartedAt:        time.Now(),
		},
		model.BuildQueueOutbox{
			ID:         outboxID,
			BuildID:    buildID,
			Exchange:   buildqueue.ExchangeName,
			RoutingKey: buildqueue.RoutingKey,
			Payload:    payload,
		},
	)
	require.NoError(t, err)

	build, err := repo.GetByID(ctx, buildID)
	require.NoError(t, err)
	require.Equal(t, model.BuildStatusPending, build.Status)
	require.Equal(t, "build-archives/demo.zip", build.ArchiveObjectKey)

	grants := repository.NewRegistryGrantRepository(db, db)
	hash := sha256.Sum256([]byte("build-secret"))
	started, didStart, err := grants.ClaimBuild(ctx, buildID, hash[:], time.Minute, 10)
	require.NoError(t, err)
	require.True(t, didStart)
	require.Equal(t, model.BuildStatusRunning, started.Status)

	again, didStart, err := grants.ClaimBuild(ctx, buildID, hash[:], time.Minute, 10)
	require.NoError(t, err)
	require.False(t, didStart)
	require.Equal(t, model.BuildStatusRunning, again.Status)

	outbox := leaseOneBuildOutbox(t, db)
	require.Equal(t, outboxID, outbox.ID)
	require.Equal(t, model.BuildOutboxStatusPublishing, outbox.Status)
}

func TestBuildRepositoryAllowsBuildingReplacementAndPromotesNewImage(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	buildRepo := repository.NewBuildRepository(db, db)
	imageRepo := repository.NewImageRepository(db)

	ownerID := uuid.New()
	oldImageID := uuid.New()
	newImageID := uuid.New()
	buildID := uuid.New()
	tag := "demo:latest"

	// The replaced image was built before migration 0019: its Registry tag is
	// its user tag's version.
	require.NoError(t, imageRepo.Save(ctx, model.Image{
		ID:          oldImageID,
		OwnerID:     ownerID,
		Tag:         tag,
		RegistryTag: "latest",
		SizeMB:      12,
		Status:      model.ImageStatusAvailable,
	}))

	payload, err := json.Marshal(buildqueue.ImageBuildMessage{
		BuildID: buildID.String(),
		ImageID: newImageID.String(),
		OwnerID: ownerID.String(),
		Tag:     tag,
	})
	require.NoError(t, err)
	require.NoError(t, buildRepo.CreateQueuedBuild(ctx,
		model.Image{ID: newImageID, RegistryTag: newImageID.String(), OwnerID: ownerID, Tag: tag, Status: model.ImageStatusBuilding},
		model.Build{
			ID:               buildID,
			ImageID:          newImageID,
			OwnerID:          ownerID,
			Status:           model.BuildStatusPending,
			LogObjectKey:     "build-logs/replacement.log",
			ArchiveObjectKey: "build-archives/replacement.zip",
			StartedAt:        time.Now(),
		},
		model.BuildQueueOutbox{ID: uuid.New(), BuildID: buildID, Exchange: buildqueue.ExchangeName, RoutingKey: buildqueue.RoutingKey, Payload: payload},
	))

	sizeMB, err := imageRepo.GetReplacementImageSizeMB(ctx, ownerID, tag, newImageID)
	require.NoError(t, err)
	require.Equal(t, int64(12), sizeMB)

	digest := "sha256:new"
	replaced, err := imageRepo.UpdateBuildAndImageSizeTx(ctx, buildID, newImageID, model.BuildStatusSuccess, 20, &digest)
	require.NoError(t, err)
	require.Equal(t, []model.Image{{ID: oldImageID, OwnerID: ownerID, Tag: tag, RegistryTag: "latest"}}, replaced)

	_, err = imageRepo.GetByID(ctx, oldImageID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	newImage, err := imageRepo.GetByID(ctx, newImageID)
	require.NoError(t, err)
	require.Equal(t, model.ImageStatusAvailable, newImage.Status)
	require.Equal(t, 20, newImage.SizeMB)
	require.Equal(t, newImageID.String(), newImage.RegistryTag)
	require.NotNil(t, newImage.Digest)
	require.Equal(t, "sha256:new", *newImage.Digest)
	// The replaced manifest is queued for cleanup in the same transaction.
	require.Equal(t, 1, countRowsWhere(t, db, "registry_cleanup_jobs", "id = $1 AND owner_id = $2 AND repository = $3 AND tag = 'latest' AND not_before <= clock_timestamp()", oldImageID, ownerID, ownerID.String()+"_demo"))

	// A redelivered completion changes nothing and replaces nothing.
	replaced, err = imageRepo.UpdateBuildAndImageSizeTx(ctx, buildID, newImageID, model.BuildStatusSuccess, 20, &digest)
	require.NoError(t, err)
	require.Empty(t, replaced)
	build, err := buildRepo.GetByID(ctx, buildID)
	require.NoError(t, err)
	require.Equal(t, model.BuildStatusSuccess, build.Status)
	require.Equal(t, 1, countRowsWhere(t, db, "images", "owner_id = $1 AND tag = $2", ownerID, tag))
}

func TestProjectRepositoryCreateComposeDeploymentAndCancel(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewProjectRepository(db)

	ownerID := uuid.New()
	projectID := uuid.New()
	jobID := uuid.New()
	err := repo.CreateWithComposeDeploymentJob(ctx,
		model.Project{ID: projectID, OwnerID: ownerID, Name: "demo", Status: model.ProjectStatusBuilding},
		model.ComposeDeploymentJob{
			ID:              jobID,
			ProjectID:       projectID,
			OwnerID:         ownerID,
			SourceType:      model.ComposeSourceTypeUpload,
			SourceObjectKey: "compose-sources/demo.zip",
			ComposeFile:     "docker-compose.yml",
			RequestID:       "req-1",
		},
		model.ComposeDeploymentOutbox{
			ID:         uuid.New(),
			JobID:      jobID,
			Exchange:   queue.ComposeExchangeName,
			RoutingKey: queue.ComposeRoutingKey,
			Payload:    []byte(`{"job_id":"` + jobID.String() + `"}`),
		},
	)
	require.NoError(t, err)

	job, started, err := repo.StartComposeDeploymentJob(ctx, jobID)
	require.NoError(t, err)
	require.True(t, started)
	require.Equal(t, model.ComposeDeploymentStatusRunning, job.Status)
	require.Equal(t, 1, job.Attempts)
	require.NotNil(t, job.StartedAt)

	require.NoError(t, repo.RequestComposeDeploymentCancel(ctx, projectID))
	job, err = repo.GetComposeDeploymentJob(ctx, jobID)
	require.NoError(t, err)
	require.True(t, job.CancelRequested)
	require.Equal(t, model.ComposeDeploymentStatusCanceling, job.Status)
}

func TestProjectRepositoryRequeueComposeDeploymentJobOnlyResetsRunningJob(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewProjectRepository(db)

	createJob := func(t *testing.T) (uuid.UUID, uuid.UUID) {
		t.Helper()
		ownerID, projectID, jobID := uuid.New(), uuid.New(), uuid.New()
		require.NoError(t, repo.CreateWithComposeDeploymentJob(ctx,
			model.Project{ID: projectID, OwnerID: ownerID, Name: "demo-" + jobID.String()[:8], Status: model.ProjectStatusBuilding},
			model.ComposeDeploymentJob{ID: jobID, ProjectID: projectID, OwnerID: ownerID, SourceType: model.ComposeSourceTypeUpload, SourceObjectKey: "compose-sources/demo.zip"},
			model.ComposeDeploymentOutbox{ID: uuid.New(), JobID: jobID, Exchange: queue.ComposeExchangeName, RoutingKey: queue.ComposeRoutingKey, Payload: []byte(`{}`)},
		))
		_, started, err := repo.StartComposeDeploymentJob(ctx, jobID)
		require.NoError(t, err)
		require.True(t, started)
		return projectID, jobID
	}

	_, runningID := createJob(t)
	require.NoError(t, repo.RequeueComposeDeploymentJob(ctx, runningID))
	job, err := repo.GetComposeDeploymentJob(ctx, runningID)
	require.NoError(t, err)
	require.Equal(t, model.ComposeDeploymentStatusQueued, job.Status)
	job, started, err := repo.StartComposeDeploymentJob(ctx, runningID)
	require.NoError(t, err)
	require.True(t, started, "a requeued job can be started again")
	require.Equal(t, 2, job.Attempts)

	cancelingProjectID, cancelingID := createJob(t)
	require.NoError(t, repo.RequestComposeDeploymentCancel(ctx, cancelingProjectID))
	require.NoError(t, repo.RequeueComposeDeploymentJob(ctx, cancelingID))
	job, err = repo.GetComposeDeploymentJob(ctx, cancelingID)
	require.NoError(t, err)
	require.Equal(t, model.ComposeDeploymentStatusCanceling, job.Status)
}

func TestProjectRepositoryRecoverLeavesDurableComposeStageRunning(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewProjectRepository(db)

	ownerID := uuid.New()
	projectID := uuid.New()
	jobID := uuid.New()
	err := repo.CreateWithComposeDeploymentJob(ctx,
		model.Project{ID: projectID, OwnerID: ownerID, Name: "demo", Status: model.ProjectStatusBuilding},
		model.ComposeDeploymentJob{
			ID:              jobID,
			ProjectID:       projectID,
			OwnerID:         ownerID,
			SourceType:      model.ComposeSourceTypeUpload,
			SourceObjectKey: "compose-sources/demo.zip",
			Status:          model.ComposeDeploymentStatusQueued,
		},
		model.ComposeDeploymentOutbox{ID: uuid.New(), JobID: jobID, Exchange: queue.ComposeExchangeName, RoutingKey: queue.ComposeRoutingKey, Payload: []byte(`{}`)},
	)
	require.NoError(t, err)
	_, started, err := repo.StartComposeDeploymentJob(ctx, jobID)
	require.NoError(t, err)
	require.True(t, started)

	planJSON := []byte(`{"project_name":"demo","services":[],"volumes":[]}`)
	resourceJSON := []byte(`{"container_ids":{}}`)
	require.NoError(t, repo.SaveComposeDeploymentPlan(ctx, jobID, model.ComposeDeploymentStageCreating, planJSON, resourceJSON))

	require.NoError(t, repo.RecoverInterruptedComposeDeployments(ctx, 1, "deployment interrupted by core service restart"))

	job, err := repo.GetComposeDeploymentJob(ctx, jobID)
	require.NoError(t, err)
	require.Equal(t, model.ComposeDeploymentStatusRunning, job.Status)
	require.Equal(t, model.ComposeDeploymentStageCreating, job.Stage)
	require.JSONEq(t, string(planJSON), string(job.PlanJSON))
}

func TestProjectRepositoryRecoverStageEmptyKeepsOriginalError(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewProjectRepository(db)

	ownerID := uuid.New()
	projectID := uuid.New()
	jobID := uuid.New()
	err := repo.CreateWithComposeDeploymentJob(ctx,
		model.Project{ID: projectID, OwnerID: ownerID, Name: "demo", Status: model.ProjectStatusBuilding},
		model.ComposeDeploymentJob{
			ID:              jobID,
			ProjectID:       projectID,
			OwnerID:         ownerID,
			SourceType:      model.ComposeSourceTypeUpload,
			SourceObjectKey: "compose-sources/demo.zip",
		},
		model.ComposeDeploymentOutbox{ID: uuid.New(), JobID: jobID, Exchange: queue.ComposeExchangeName, RoutingKey: queue.ComposeRoutingKey, Payload: []byte(`{}`)},
	)
	require.NoError(t, err)
	_, started, err := repo.StartComposeDeploymentJob(ctx, jobID)
	require.NoError(t, err)
	require.True(t, started)

	originalErr := "container create for service migrate failed: network already exists"
	_, err = db.Exec(ctx, `UPDATE compose_deployment_jobs SET error_message = $1 WHERE id = $2`, originalErr, jobID)
	require.NoError(t, err)

	require.NoError(t, repo.RecoverInterruptedComposeDeployments(ctx, 1, "deployment interrupted by core service restart"))

	job, err := repo.GetComposeDeploymentJob(ctx, jobID)
	require.NoError(t, err)
	require.Equal(t, model.ComposeDeploymentStatusFailed, job.Status)
	require.NotNil(t, job.ErrorMessage)
	require.Equal(t, originalErr, *job.ErrorMessage)

	project, err := repo.GetByID(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, model.ProjectStatusFailed, project.Status)
	require.NotNil(t, project.ErrorMessage)
	require.Equal(t, originalErr, *project.ErrorMessage)
}

func TestStagedObjectRepositoryReserveQuotaAndRelease(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewStagedObjectRepository(db)
	ownerID := uuid.New()

	err := repo.Reserve(ctx, model.StagedObjectReservation{
		ID:            uuid.New(),
		OwnerID:       ownerID,
		ObjectKey:     "build-archives/demo.zip",
		Kind:          model.StagedObjectKindBuildArchive,
		BytesReserved: 128,
	}, 256)
	require.NoError(t, err)

	used, err := repo.ActiveBytesByOwner(ctx, ownerID)
	require.NoError(t, err)
	require.EqualValues(t, 128, used)

	err = repo.Reserve(ctx, model.StagedObjectReservation{
		ID:            uuid.New(),
		OwnerID:       ownerID,
		ObjectKey:     "build-archives/too-large.zip",
		Kind:          model.StagedObjectKindBuildArchive,
		BytesReserved: 129,
	}, 256)
	require.ErrorIs(t, err, apperrors.ErrQuotaExceeded)

	require.NoError(t, repo.Release(ctx, "build-archives/demo.zip"))
	used, err = repo.ActiveBytesByOwner(ctx, ownerID)
	require.NoError(t, err)
	require.Zero(t, used)
	require.ErrorIs(t, repo.Release(ctx, "build-archives/demo.zip"), apperrors.ErrNotFound)
}

func leaseOneBuildOutbox(t *testing.T, db *pgxpool.Pool) model.BuildQueueOutbox {
	t.Helper()
	repo := repository.NewBuildRepository(db, db)
	entries, err := repo.LeasePendingBuildOutbox(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	if entries[0].Status != model.BuildOutboxStatusPublishing {
		var status string
		err := db.QueryRow(context.Background(), `SELECT status FROM build_queue_outbox WHERE id = $1`, entries[0].ID).Scan(&status)
		require.NoError(t, err)
		entries[0].Status = status
	}
	return entries[0]
}

func countRowsWhere(t *testing.T, db *pgxpool.Pool, table, where string, args ...any) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRow(context.Background(), `SELECT COUNT(*) FROM `+table+` WHERE `+where, args...).Scan(&count))
	return count
}

func TestBuildRepositoryListsAnOwnersActiveBuilds(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewBuildRepository(db, db)
	ownerID := uuid.New()
	queue := func(owner uuid.UUID, status string) uuid.UUID {
		imageID, buildID := uuid.New(), uuid.New()
		require.NoError(t, repo.CreateQueuedBuild(ctx,
			model.Image{ID: imageID, RegistryTag: imageID.String(), OwnerID: owner, Tag: "repo/" + buildID.String()[:8] + ":latest"},
			model.Build{ID: buildID, ImageID: imageID, OwnerID: owner, Status: model.BuildStatusPending, LogObjectKey: "build-logs/x.log"},
			model.BuildQueueOutbox{ID: uuid.New(), BuildID: buildID, Exchange: buildqueue.ExchangeName, RoutingKey: buildqueue.RoutingKey, Payload: []byte(`{}`)},
		))
		if status != model.BuildStatusPending {
			require.NoError(t, repo.UpdateStatus(ctx, buildID, status))
		}
		return buildID
	}
	pending := queue(ownerID, model.BuildStatusPending)
	running := queue(ownerID, model.BuildStatusRunning)
	for _, status := range []string{model.BuildStatusSuccess, model.BuildStatusFailed, model.BuildStatusCanceled, model.BuildStatusFailedResourceExhausted} {
		queue(ownerID, status)
	}
	queue(uuid.New(), model.BuildStatusRunning)

	builds, err := repo.ListActiveByOwner(ctx, ownerID)

	require.NoError(t, err)
	ids := make([]uuid.UUID, 0, len(builds))
	for _, build := range builds {
		require.Equal(t, ownerID, build.OwnerID)
		ids = append(ids, build.ID)
	}
	require.ElementsMatch(t, []uuid.UUID{pending, running}, ids, "only the owner's pending and running builds are active")
}
