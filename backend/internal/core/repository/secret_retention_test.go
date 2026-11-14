package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const buildOutboxSecretPayload = `{"build_args": {"NPM_TOKEN": "s3cret"}, "build_id": "b"}`

func queueBuildWithSecretPayload(t *testing.T, db *pgxpool.Pool) (*repository.BuildRepository, uuid.UUID) {
	t.Helper()
	repo := repository.NewBuildRepository(db, db)
	owner, imageID, buildID, outboxID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	require.NoError(t, repo.CreateQueuedBuild(context.Background(),
		model.Image{ID: imageID, RegistryTag: imageID.String(), OwnerID: owner, Tag: "app:latest"},
		model.Build{ID: buildID, ImageID: imageID, OwnerID: owner, Status: model.BuildStatusPending},
		model.BuildQueueOutbox{ID: outboxID, BuildID: buildID, Exchange: "x", RoutingKey: "k", Payload: []byte(buildOutboxSecretPayload)}))
	return repo, outboxID
}

func storedBuildOutboxPayload(t *testing.T, db *pgxpool.Pool, outboxID uuid.UUID) string {
	t.Helper()
	var payload string
	require.NoError(t, db.QueryRow(context.Background(), `SELECT payload::text FROM build_queue_outbox WHERE id = $1`, outboxID).Scan(&payload))
	return payload
}

func TestBuildOutboxPayloadIsEmptiedOncePublished(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo, outboxID := queueBuildWithSecretPayload(t, db)
	leased := leaseOneBuildOutbox(t, db)
	require.JSONEq(t, buildOutboxSecretPayload, string(leased.Payload), "the relay publishes the full message")

	require.NoError(t, repo.MarkBuildOutboxPublished(ctx, outboxID))

	require.JSONEq(t, `{}`, storedBuildOutboxPayload(t, db, outboxID))
}

func TestBuildOutboxPayloadIsEmptiedOnceDiscarded(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo, outboxID := queueBuildWithSecretPayload(t, db)
	leaseOneBuildOutbox(t, db)

	require.NoError(t, repo.MarkBuildOutboxDiscarded(ctx, outboxID, errors.New("build is terminal")))

	require.JSONEq(t, `{}`, storedBuildOutboxPayload(t, db, outboxID))
}

func TestBuildOutboxFailedPublishKeepsThePayloadForTheRetry(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo, outboxID := queueBuildWithSecretPayload(t, db)
	leaseOneBuildOutbox(t, db)

	require.NoError(t, repo.MarkBuildOutboxPending(ctx, outboxID, errors.New("broker unavailable")))

	retried := leaseOneBuildOutbox(t, db)
	require.Equal(t, outboxID, retried.ID)
	require.JSONEq(t, buildOutboxSecretPayload, string(retried.Payload))
}

// A published row's payload is gone, so nothing may queue it again: the row
// would publish an empty message.
func TestBuildOutboxPublishedRowNeverReturnsToPending(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo, outboxID := queueBuildWithSecretPayload(t, db)
	leaseOneBuildOutbox(t, db)
	require.NoError(t, repo.MarkBuildOutboxPublished(ctx, outboxID))

	err := repo.MarkBuildOutboxPending(ctx, outboxID, errors.New("late failure"))

	require.ErrorIs(t, err, apperrors.ErrNotFound)
	entries, err := repo.LeasePendingBuildOutbox(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestComposeDeploymentPlanIsRemovedWhenTheJobEnds(t *testing.T) {
	for _, status := range []string{
		model.ComposeDeploymentStatusSucceeded,
		model.ComposeDeploymentStatusFailed,
		model.ComposeDeploymentStatusCanceled,
	} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			db := dbtest.OpenCorePostgres(t)
			repo := repository.NewProjectRepository(db)
			ownerID, projectID, jobID := uuid.New(), uuid.New(), uuid.New()
			require.NoError(t, repo.CreateWithComposeDeploymentJob(ctx,
				model.Project{ID: projectID, OwnerID: ownerID, Name: "demo", Status: model.ProjectStatusBuilding},
				model.ComposeDeploymentJob{ID: jobID, ProjectID: projectID, OwnerID: ownerID, SourceType: model.ComposeSourceTypeUpload, SourceObjectKey: "compose-sources/demo.zip", Status: model.ComposeDeploymentStatusQueued},
				model.ComposeDeploymentOutbox{ID: uuid.New(), JobID: jobID, Exchange: queue.ComposeExchangeName, RoutingKey: queue.ComposeRoutingKey, Payload: []byte(`{}`)}))
			_, started, err := repo.StartComposeDeploymentJob(ctx, jobID)
			require.NoError(t, err)
			require.True(t, started)
			resourceMap := []byte(`{"container_ids": {"web": "c1"}}`)
			require.NoError(t, repo.SaveComposeDeploymentPlan(ctx, jobID, model.ComposeDeploymentStageCreating,
				[]byte(`{"services": [{"env_vars": {"DB_PASSWORD": "s3cret"}}]}`), resourceMap))

			require.NoError(t, repo.CompleteComposeDeploymentJob(ctx, jobID, status, nil))

			job, err := repo.GetComposeDeploymentJob(ctx, jobID)
			require.NoError(t, err)
			require.Equal(t, status, job.Status)
			require.Empty(t, job.PlanJSON, "the plan holds environment values and build arguments")
			require.JSONEq(t, string(resourceMap), string(job.ResourceMapJSON), "the resource map holds only IDs and stays")
		})
	}
}
