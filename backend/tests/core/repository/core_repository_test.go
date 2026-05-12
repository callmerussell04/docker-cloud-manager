package repository_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/composequeue"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCoreMigrationsApply(t *testing.T) {
	db := dbtest.OpenCorePostgres(t)

	require.GreaterOrEqual(t, dbtest.CountRows(t, db, "projects"), 0)
}

func TestBuildRepositoryCreateQueuedBuildPersistsTransaction(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewBuildRepository(db)

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
		model.Image{ID: imageID, OwnerID: ownerID, Tag: "repo/demo:latest"},
		model.Build{
			ID:               buildID,
			ImageID:          imageID,
			OwnerID:          ownerID,
			Status:           model.BuildStatusPending,
			LogFilePath:      "build-logs/demo.log",
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

	started, didStart, err := repo.StartBuild(ctx, buildID)
	require.NoError(t, err)
	require.True(t, didStart)
	require.Equal(t, model.BuildStatusRunning, started.Status)

	again, didStart, err := repo.StartBuild(ctx, buildID)
	require.NoError(t, err)
	require.False(t, didStart)
	require.Equal(t, model.BuildStatusRunning, again.Status)

	outbox := leaseOneBuildOutbox(t, db)
	require.Equal(t, outboxID, outbox.ID)
	require.Equal(t, model.BuildOutboxStatusPublishing, outbox.Status)
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
			Exchange:   composequeue.ExchangeName,
			RoutingKey: composequeue.RoutingKey,
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

func leaseOneBuildOutbox(t *testing.T, db *sql.DB) model.BuildQueueOutbox {
	t.Helper()
	repo := repository.NewBuildRepository(db)
	entries, err := repo.LeasePendingBuildOutbox(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	if entries[0].Status != model.BuildOutboxStatusPublishing {
		var status string
		err := db.QueryRow(`SELECT status FROM build_queue_outbox WHERE id = $1`, entries[0].ID).Scan(&status)
		require.NoError(t, err)
		entries[0].Status = status
	}
	return entries[0]
}
