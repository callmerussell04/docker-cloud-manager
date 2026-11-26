package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func backdateComposeOutbox(t *testing.T, db *pgxpool.Pool, jobID uuid.UUID, age time.Duration) {
	t.Helper()
	_, err := db.Exec(context.Background(), `UPDATE compose_deployment_queue_outbox SET updated_at = updated_at - make_interval(secs => $2) WHERE job_id = $1`, jobID, age.Seconds())
	require.NoError(t, err)
}

func composeOutboxStatus(t *testing.T, db *pgxpool.Pool, jobID uuid.UUID) string {
	t.Helper()
	var status string
	require.NoError(t, db.QueryRow(context.Background(), `SELECT status FROM compose_deployment_queue_outbox WHERE job_id = $1`, jobID).Scan(&status))
	return status
}

func TestProjectRepositoryRepublishesLostComposeDeliveries(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewProjectRepository(db)
	settle := func(row composeJobRow, published bool) {
		leased, err := repo.LeasePendingComposeOutbox(ctx, 100)
		require.NoError(t, err)
		require.Len(t, leased, 1)
		if published {
			require.NoError(t, repo.MarkComposeOutboxPublished(ctx, row.outboxID))
		} else {
			require.NoError(t, repo.MarkComposeOutboxDiscarded(ctx, row.outboxID, nil))
		}
	}
	// Queued, published long ago: its message is gone.
	queued := createComposeJob(t, repo, "queued")
	settle(queued, true)
	backdateComposeOutbox(t, db, queued.jobID, recoveryExpired)
	// Queued, discarded long ago.
	discarded := createComposeJob(t, repo, "discarded")
	settle(discarded, false)
	backdateComposeOutbox(t, db, discarded.jobID, recoveryExpired)
	// Canceled while queued: the cancellation is carried out at delivery.
	canceled := createComposeJob(t, repo, "canceled")
	settle(canceled, true)
	require.NoError(t, repo.RequestComposeDeploymentCancel(ctx, canceled.projectID))
	backdateComposeOutbox(t, db, canceled.jobID, recoveryExpired)
	// Published recently: the message may still be queued.
	recent := createComposeJob(t, repo, "recent")
	settle(recent, true)
	backdateComposeOutbox(t, db, recent.jobID, recoveryLostAfter-time.Minute)
	// A started job, canceled or not, belongs to its worker or to startup recovery.
	running := createComposeJob(t, repo, "running")
	settle(running, true)
	startComposeJob(t, repo, running, "")
	backdateComposeOutbox(t, db, running.jobID, recoveryExpired)
	canceling := createComposeJob(t, repo, "canceling")
	settle(canceling, true)
	startComposeJob(t, repo, canceling, "")
	require.NoError(t, repo.RequestComposeDeploymentCancel(ctx, canceling.projectID))
	backdateComposeOutbox(t, db, canceling.jobID, recoveryExpired)
	// A record still pending belongs to the relay.
	unpublished := createComposeJob(t, repo, "unpublished")
	backdateComposeOutbox(t, db, unpublished.jobID, recoveryExpired)

	got, err := repo.RepublishLostComposeDeliveries(ctx, recoveryLostAfter)

	require.NoError(t, err)
	require.ElementsMatch(t, []model.RecoveredDeployment{
		{JobID: queued.jobID, ProjectID: queued.projectID},
		{JobID: discarded.jobID, ProjectID: discarded.projectID},
		{JobID: canceled.jobID, ProjectID: canceled.projectID},
	}, got)
	for _, row := range []composeJobRow{queued, discarded, canceled, unpublished} {
		require.Equal(t, model.ComposeOutboxStatusPending, composeOutboxStatus(t, db, row.jobID))
	}
	for _, row := range []composeJobRow{recent, running, canceling} {
		require.Equal(t, model.ComposeOutboxStatusPublished, composeOutboxStatus(t, db, row.jobID))
	}
	leased, err := repo.LeasePendingComposeOutbox(ctx, 100)
	require.NoError(t, err)
	for _, item := range leased {
		require.JSONEq(t, `{"job_id":"`+item.JobID.String()+`"}`, string(item.Payload))
	}
}
