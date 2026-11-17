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

// composeJobRow is one Compose deployment created through the repository.
type composeJobRow struct {
	projectID uuid.UUID
	jobID     uuid.UUID
	outboxID  uuid.UUID
}

// createComposeJob creates a queued Compose deployment with its project and
// outbox row, as upload does.
func createComposeJob(t *testing.T, repo *repository.ProjectRepository, name string) composeJobRow {
	t.Helper()
	ownerID := uuid.New()
	row := composeJobRow{projectID: uuid.New(), jobID: uuid.New(), outboxID: uuid.New()}
	require.NoError(t, repo.CreateWithComposeDeploymentJob(context.Background(),
		model.Project{ID: row.projectID, OwnerID: ownerID, Name: name, Status: model.ProjectStatusBuilding},
		model.ComposeDeploymentJob{ID: row.jobID, ProjectID: row.projectID, OwnerID: ownerID, SourceType: model.ComposeSourceTypeUpload, SourceObjectKey: "compose-sources/" + name + ".zip"},
		model.ComposeDeploymentOutbox{ID: row.outboxID, JobID: row.jobID, Exchange: queue.ComposeExchangeName, RoutingKey: queue.ComposeRoutingKey, Payload: []byte(`{"job_id":"` + row.jobID.String() + `"}`)},
	))
	return row
}

// startComposeJob moves a queued job to running and, when stage is set,
// persists its plan at that stage.
func startComposeJob(t *testing.T, repo *repository.ProjectRepository, row composeJobRow, stage string) {
	t.Helper()
	ctx := context.Background()
	_, started, err := repo.StartComposeDeploymentJob(ctx, row.jobID)
	require.NoError(t, err)
	require.True(t, started)
	if stage != "" {
		require.NoError(t, repo.SaveComposeDeploymentPlan(ctx, row.jobID, stage, []byte(`{"project_name":"demo"}`), []byte(`{}`)))
	}
}

func jobIDs(jobs []model.ComposeDeploymentJob) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(jobs))
	for _, job := range jobs {
		ids = append(ids, job.ID)
	}
	return ids
}

func TestProjectRepositoryLeasesComposeOutbox(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewProjectRepository(db)
	first := createComposeJob(t, repo, "first")
	second := createComposeJob(t, repo, "second")
	third := createComposeJob(t, repo, "third")

	leased, err := repo.LeasePendingComposeOutbox(ctx, 2)
	require.NoError(t, err)
	require.Len(t, leased, 2, "the batch size bounds a lease")
	require.ElementsMatch(t, []uuid.UUID{first.outboxID, second.outboxID}, []uuid.UUID{leased[0].ID, leased[1].ID}, "the oldest rows lease first")
	for _, item := range leased {
		require.Equal(t, model.ComposeOutboxStatusPublishing, item.Status)
		require.Equal(t, 1, item.Attempts)
		require.Equal(t, queue.ComposeExchangeName, item.Exchange)
		require.Equal(t, queue.ComposeRoutingKey, item.RoutingKey)
		require.JSONEq(t, `{"job_id":"`+item.JobID.String()+`"}`, string(item.Payload))
	}

	leased, err = repo.LeasePendingComposeOutbox(ctx, 10)
	require.NoError(t, err)
	require.Len(t, leased, 1, "a fresh lease is not leased again")
	require.Equal(t, third.outboxID, leased[0].ID)

	require.NoError(t, repo.MarkComposeOutboxPublished(ctx, first.outboxID))
	require.NoError(t, repo.MarkComposeOutboxDiscarded(ctx, second.outboxID, errors.New("job is terminal")))
	require.NoError(t, repo.MarkComposeOutboxPending(ctx, third.outboxID, errors.New("broker unavailable")))
	agePublishing(t, db, first.outboxID, second.outboxID)

	leased, err = repo.LeasePendingComposeOutbox(ctx, 10)
	require.NoError(t, err)
	require.Len(t, leased, 1, "published and discarded rows never lease, however old")
	require.Equal(t, third.outboxID, leased[0].ID, "a row returned to pending leases again")
	require.Equal(t, 2, leased[0].Attempts)
	require.NotNil(t, leased[0].LastError)
	require.Equal(t, "broker unavailable", *leased[0].LastError)

	leased, err = repo.LeasePendingComposeOutbox(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, leased)
	agePublishing(t, db, third.outboxID)
	leased, err = repo.LeasePendingComposeOutbox(ctx, 10)
	require.NoError(t, err)
	require.Len(t, leased, 1, "a lease left publishing for over a minute is leased again")
	require.Equal(t, third.outboxID, leased[0].ID)
	require.Equal(t, 3, leased[0].Attempts)

	require.ErrorIs(t, repo.MarkComposeOutboxPublished(ctx, uuid.New()), apperrors.ErrNotFound)
}

// agePublishing backdates outbox rows past the one-minute lease.
func agePublishing(t *testing.T, db *pgxpool.Pool, ids ...uuid.UUID) {
	t.Helper()
	_, err := db.Exec(context.Background(), `UPDATE compose_deployment_queue_outbox SET updated_at = NOW() - INTERVAL '2 minutes' WHERE id = ANY($1)`, ids)
	require.NoError(t, err)
}

func TestProjectRepositorySavesAndLoadsServiceGraph(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewProjectRepository(db)
	row := createComposeJob(t, repo, "graph")
	dbID, apiID, workerID := insertProjectContainer(t, db, row.projectID, "db"), insertProjectContainer(t, db, row.projectID, "api"), insertProjectContainer(t, db, row.projectID, "worker")

	_, err := repo.GetServiceGraph(ctx, row.projectID)
	require.ErrorIs(t, err, apperrors.ErrNotFound, "a project without a saved graph has none")

	graph := []model.ProjectServiceNode{
		{ContainerID: dbID, ServiceName: "db", StartOrder: 0},
		{ContainerID: apiID, ServiceName: "api", StartOrder: 1, Dependencies: []model.ProjectServiceDependency{
			{DependsOnContainerID: dbID, Condition: model.ComposeDependencyConditionHealthy},
		}},
		{ContainerID: workerID, ServiceName: "worker", StartOrder: 2, Dependencies: []model.ProjectServiceDependency{
			{DependsOnContainerID: dbID, Condition: model.ComposeDependencyConditionStarted, Optional: true},
			{DependsOnContainerID: apiID, Condition: model.ComposeDependencyConditionCompletedSuccessfully},
		}},
	}
	require.NoError(t, repo.SaveServiceGraph(ctx, row.projectID, graph))

	loaded, err := repo.GetServiceGraph(ctx, row.projectID)
	require.NoError(t, err)
	require.Len(t, loaded, 3)
	require.Equal(t, []string{"db", "api", "worker"}, []string{loaded[0].ServiceName, loaded[1].ServiceName, loaded[2].ServiceName}, "services load in start order")
	require.Empty(t, loaded[0].Dependencies)
	require.Equal(t, []model.ProjectServiceDependency{{
		ProjectID: row.projectID, ContainerID: apiID, DependsOnContainerID: dbID, DependsOnServiceName: "db", Condition: model.ComposeDependencyConditionHealthy,
	}}, loaded[1].Dependencies)
	require.ElementsMatch(t, []model.ProjectServiceDependency{
		{ProjectID: row.projectID, ContainerID: workerID, DependsOnContainerID: dbID, DependsOnServiceName: "db", Condition: model.ComposeDependencyConditionStarted, Optional: true},
		{ProjectID: row.projectID, ContainerID: workerID, DependsOnContainerID: apiID, DependsOnServiceName: "api", Condition: model.ComposeDependencyConditionCompletedSuccessfully},
	}, loaded[2].Dependencies)
	for _, node := range loaded {
		require.Equal(t, row.projectID, node.ProjectID)
	}

	// Saving again replaces the whole graph.
	require.NoError(t, repo.SaveServiceGraph(ctx, row.projectID, []model.ProjectServiceNode{{ContainerID: apiID, ServiceName: "api", StartOrder: 0}}))
	loaded, err = repo.GetServiceGraph(ctx, row.projectID)
	require.NoError(t, err)
	require.Equal(t, []model.ProjectServiceNode{{ProjectID: row.projectID, ContainerID: apiID, ServiceName: "api", StartOrder: 0}}, loaded)

	// An invalid dependency condition rejects the save and keeps the saved graph.
	err = repo.SaveServiceGraph(ctx, row.projectID, []model.ProjectServiceNode{
		{ContainerID: dbID, ServiceName: "db", StartOrder: 0},
		{ContainerID: apiID, ServiceName: "api", StartOrder: 1, Dependencies: []model.ProjectServiceDependency{{DependsOnContainerID: dbID, Condition: "service_ready"}}},
	})
	require.ErrorIs(t, err, apperrors.ErrBadRequest)
	loaded, err = repo.GetServiceGraph(ctx, row.projectID)
	require.NoError(t, err)
	require.Equal(t, []model.ProjectServiceNode{{ProjectID: row.projectID, ContainerID: apiID, ServiceName: "api", StartOrder: 0}}, loaded)
}

// insertProjectContainer records a Compose service container for the
// service graph's foreign keys.
func insertProjectContainer(t *testing.T, db *pgxpool.Pool, projectID uuid.UUID, service string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(context.Background(), `
		INSERT INTO containers (id, owner_id, project_id, name, image_tag, status, desired_status, base_memory_reservation)
		VALUES ($1, $2, $3, $4, 'nginx:latest', 'created', 'created', 268435456)`,
		id, uuid.New(), projectID, "graph_"+service)
	require.NoError(t, err)
	return id
}

func TestProjectRepositoryListsActiveAndInterruptedComposeJobs(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewProjectRepository(db)

	queued := createComposeJob(t, repo, "queued")
	planning := createComposeJob(t, repo, "planning")
	startComposeJob(t, repo, planning, "")
	creating := createComposeJob(t, repo, "creating")
	startComposeJob(t, repo, creating, model.ComposeDeploymentStageCreating)
	building := createComposeJob(t, repo, "building")
	startComposeJob(t, repo, building, model.ComposeDeploymentStageBuilding)
	canceling := createComposeJob(t, repo, "canceling")
	startComposeJob(t, repo, canceling, model.ComposeDeploymentStageStarting)
	require.NoError(t, repo.RequestComposeDeploymentCancel(ctx, canceling.projectID))
	cancelingUnplanned := createComposeJob(t, repo, "canceling-unplanned")
	startComposeJob(t, repo, cancelingUnplanned, "")
	require.NoError(t, repo.RequestComposeDeploymentCancel(ctx, cancelingUnplanned.projectID))
	for _, status := range []string{model.ComposeDeploymentStatusSucceeded, model.ComposeDeploymentStatusFailed, model.ComposeDeploymentStatusCanceled} {
		done := createComposeJob(t, repo, status)
		startComposeJob(t, repo, done, model.ComposeDeploymentStageStarting)
		require.NoError(t, repo.CompleteComposeDeploymentJob(ctx, done.jobID, status, nil))
	}
	// Order by last update: canceling, then building, then creating.
	setJobUpdatedAt(t, db, canceling.jobID, "3 minutes")
	setJobUpdatedAt(t, db, building.jobID, "2 minutes")
	setJobUpdatedAt(t, db, creating.jobID, "1 minute")

	active, err := repo.ListActiveComposeDeploymentJobs(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{canceling.jobID, building.jobID, creating.jobID}, jobIDs(active),
		"running and canceling jobs with a durable stage are active, least recently updated first")
	require.Equal(t, model.ComposeDeploymentStatusCanceling, active[0].Status)
	require.True(t, active[0].CancelRequested)
	require.Equal(t, model.ComposeDeploymentStageBuilding, active[1].Stage)
	require.JSONEq(t, `{"project_name":"demo"}`, string(active[1].PlanJSON))

	active, err = repo.ListActiveComposeDeploymentJobs(ctx, 2)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{canceling.jobID, building.jobID}, jobIDs(active), "the limit keeps the least recently updated")

	interrupted, err := repo.ListInterruptedComposeDeploymentJobs(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{planning.jobID, creating.jobID, building.jobID, canceling.jobID, cancelingUnplanned.jobID}, jobIDs(interrupted),
		"every running or canceling job is interrupted by a restart, staged or not")
	require.NotContains(t, jobIDs(interrupted), queued.jobID)
}

func setJobUpdatedAt(t *testing.T, db *pgxpool.Pool, jobID uuid.UUID, age string) {
	t.Helper()
	_, err := db.Exec(context.Background(), `UPDATE compose_deployment_jobs SET updated_at = NOW() - $1::interval WHERE id = $2`, age, jobID)
	require.NoError(t, err)
}

func TestProjectRepositoryListsAnOwnersCancelableComposeJobs(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewProjectRepository(db)
	ownerID := uuid.New()
	create := func(owner uuid.UUID, name string) composeJobRow {
		row := composeJobRow{projectID: uuid.New(), jobID: uuid.New(), outboxID: uuid.New()}
		require.NoError(t, repo.CreateWithComposeDeploymentJob(ctx,
			model.Project{ID: row.projectID, OwnerID: owner, Name: name, Status: model.ProjectStatusBuilding},
			model.ComposeDeploymentJob{ID: row.jobID, ProjectID: row.projectID, OwnerID: owner, SourceType: model.ComposeSourceTypeUpload, SourceObjectKey: "compose-sources/" + name + ".zip"},
			model.ComposeDeploymentOutbox{ID: row.outboxID, JobID: row.jobID, Exchange: queue.ComposeExchangeName, RoutingKey: queue.ComposeRoutingKey, Payload: []byte(`{}`)},
		))
		return row
	}
	queued := create(ownerID, "queued")
	running := create(ownerID, "running")
	startComposeJob(t, repo, running, model.ComposeDeploymentStageCreating)
	canceling := create(ownerID, "canceling")
	startComposeJob(t, repo, canceling, "")
	require.NoError(t, repo.RequestComposeDeploymentCancel(ctx, canceling.projectID))
	succeeded := create(ownerID, "succeeded")
	startComposeJob(t, repo, succeeded, model.ComposeDeploymentStageStarting)
	require.NoError(t, repo.CompleteComposeDeploymentJob(ctx, succeeded.jobID, model.ComposeDeploymentStatusSucceeded, nil))
	create(uuid.New(), "other")

	jobs, err := repo.ListCancelableComposeDeploymentJobsByOwner(ctx, ownerID)

	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{queued.jobID, running.jobID}, jobIDs(jobs),
		"the owner's queued and running jobs; one already canceling or finished is not cancelable")
	for _, job := range jobs {
		require.Equal(t, ownerID, job.OwnerID)
	}
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
