package compose_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	compose "github.com/callmerussell04/docker-cloud-manager/internal/core/service/compose"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestComposeCoordinatorDoesNotTreatQueuedStartAsStarted(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	dbID := uuid.New()
	migrateID := uuid.New()
	job := startingComposeJob(t, ownerID, projectID, dbID, migrateID, map[string]bool{})
	repo := newCoordinatorRepo(job)
	containers := newCoordinatorContainers(map[uuid.UUID]model.Container{
		dbID:      {ID: dbID, OwnerID: ownerID, DockerID: "db-docker", Status: model.ContainerStatusCreated},
		migrateID: {ID: migrateID, OwnerID: ownerID, DockerID: "migrate-docker", Status: model.ContainerStatusCreated},
	})
	docker := &coordinatorDocker{
		inspections: map[string]model.ContainerInspection{
			"db-docker": {
				Healthcheck: &model.Healthcheck{Test: []string{"CMD-SHELL", "pg_isready -U postgres"}},
				State:       model.ContainerState{Status: "created", Running: false},
			},
		},
	}
	runCoordinatorOnce(t, repo, containers, docker)

	require.Equal(t, []uuid.UUID{dbID}, containers.started)
	require.Empty(t, repo.completedStatus)
	for _, raw := range repo.savedResourceMaps {
		var resources struct {
			StartedServices map[string]bool `json:"started_services"`
		}
		require.NoError(t, json.Unmarshal(raw, &resources))
		require.False(t, resources.StartedServices["db"])
	}
}

func TestComposeCoordinatorStartsDependentServiceAfterHealthyDependency(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	dbID := uuid.New()
	migrateID := uuid.New()
	job := startingComposeJob(t, ownerID, projectID, dbID, migrateID, map[string]bool{"db": true})
	repo := newCoordinatorRepo(job)
	containers := newCoordinatorContainers(map[uuid.UUID]model.Container{
		dbID:      {ID: dbID, OwnerID: ownerID, DockerID: "db-docker", Status: model.ContainerStatusRunning},
		migrateID: {ID: migrateID, OwnerID: ownerID, DockerID: "migrate-docker", Status: model.ContainerStatusCreated},
	})
	healthy := "healthy"
	docker := &coordinatorDocker{
		inspections: map[string]model.ContainerInspection{
			"db-docker": {
				Healthcheck: &model.Healthcheck{Test: []string{"CMD-SHELL", "pg_isready -U postgres"}},
				State:       model.ContainerState{Status: "running", Running: true, HealthStatus: &healthy},
			},
		},
	}
	runCoordinatorOnce(t, repo, containers, docker)

	require.Equal(t, []uuid.UUID{migrateID}, containers.started)
	require.Empty(t, repo.completedStatus)
}

func startingComposeJob(t *testing.T, ownerID, projectID, dbID, migrateID uuid.UUID, started map[string]bool) model.ComposeDeploymentJob {
	t.Helper()
	plan := map[string]any{
		"project_name": "wh",
		"source_type":  model.ComposeSourceTypeGit,
		"services": []model.ComposeService{
			{Name: "db", ImageTag: "postgres:17-alpine"},
			{
				Name:     "migrate",
				ImageTag: "wh_migrate:latest",
				DependsOn: []model.ComposeDependency{{
					ServiceName: "db",
					Condition:   model.ComposeDependencyConditionHealthy,
				}},
			},
		},
	}
	resources := map[string]any{
		"container_ids": map[string]string{
			"db":      dbID.String(),
			"migrate": migrateID.String(),
		},
		"started_services":    started,
		"service_graph_saved": true,
	}
	planJSON, err := json.Marshal(plan)
	require.NoError(t, err)
	resourceJSON, err := json.Marshal(resources)
	require.NoError(t, err)
	return model.ComposeDeploymentJob{
		ID:              uuid.New(),
		ProjectID:       projectID,
		OwnerID:         ownerID,
		Status:          model.ComposeDeploymentStatusRunning,
		Stage:           model.ComposeDeploymentStageStarting,
		PlanJSON:        planJSON,
		ResourceMapJSON: resourceJSON,
	}
}

func runCoordinatorOnce(t *testing.T, repo *coordinatorRepo, containers *coordinatorContainers, docker *coordinatorDocker) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	repo.cancel = cancel
	orch := compose.NewOrchestrator(ctx, repo, nil, nil, containers, docker, coordinatorConfig{}, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	coordinator := compose.NewComposeDeploymentCoordinator(orch, slog.New(slog.NewTextHandler(io.Discard, nil)))
	done := make(chan struct{})
	go func() {
		defer close(done)
		coordinator.Run(ctx)
	}()
	<-done
}

type coordinatorRepo struct {
	job               model.ComposeDeploymentJob
	cancel            context.CancelFunc
	savedResourceMaps [][]byte
	completedStatus   string
}

func newCoordinatorRepo(job model.ComposeDeploymentJob) *coordinatorRepo {
	return &coordinatorRepo{job: job}
}

func (r *coordinatorRepo) ListActiveComposeDeploymentJobs(context.Context, int) ([]model.ComposeDeploymentJob, error) {
	if r.cancel != nil {
		defer r.cancel()
	}
	return []model.ComposeDeploymentJob{r.job}, nil
}
func (r *coordinatorRepo) UpdateComposeDeploymentProgress(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string, _ string, _ []byte, resourceMapJSON []byte) error {
	r.savedResourceMaps = append(r.savedResourceMaps, append([]byte(nil), resourceMapJSON...))
	return nil
}
func (r *coordinatorRepo) SaveComposeDeploymentPlan(context.Context, uuid.UUID, string, []byte, []byte) error {
	return nil
}
func (r *coordinatorRepo) CompleteComposeDeploymentJob(_ context.Context, _ uuid.UUID, status string, _ *string) error {
	r.completedStatus = status
	return nil
}
func (r *coordinatorRepo) UpdateStatus(context.Context, uuid.UUID, string, *string) error { return nil }
func (r *coordinatorRepo) Save(context.Context, model.Project) error                      { return nil }
func (r *coordinatorRepo) CreateWithComposeDeploymentJob(context.Context, model.Project, model.ComposeDeploymentJob, model.ComposeDeploymentOutbox) error {
	return nil
}
func (r *coordinatorRepo) GetByID(context.Context, uuid.UUID) (model.Project, error) {
	return model.Project{}, nil
}
func (r *coordinatorRepo) GetComposeDeploymentJob(context.Context, uuid.UUID) (model.ComposeDeploymentJob, error) {
	return r.job, nil
}
func (r *coordinatorRepo) GetActiveComposeDeploymentJobByProjectID(context.Context, uuid.UUID) (model.ComposeDeploymentJob, error) {
	return r.job, nil
}
func (r *coordinatorRepo) ListInterruptedComposeDeploymentJobs(context.Context) ([]model.ComposeDeploymentJob, error) {
	return nil, nil
}
func (r *coordinatorRepo) StartComposeDeploymentJob(context.Context, uuid.UUID) (model.ComposeDeploymentJob, bool, error) {
	return r.job, true, nil
}
func (r *coordinatorRepo) RequestComposeDeploymentCancel(context.Context, uuid.UUID) error {
	return nil
}
func (r *coordinatorRepo) SaveServiceGraph(context.Context, uuid.UUID, []model.ProjectServiceNode) error {
	return nil
}

type coordinatorContainers struct {
	mu         sync.Mutex
	containers map[uuid.UUID]model.Container
	started    []uuid.UUID
}

func newCoordinatorContainers(items map[uuid.UUID]model.Container) *coordinatorContainers {
	return &coordinatorContainers{containers: items}
}
func (c *coordinatorContainers) GetByID(_ context.Context, id uuid.UUID) (model.Container, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.containers[id], nil
}
func (c *coordinatorContainers) Start(_ context.Context, id uuid.UUID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	item := c.containers[id]
	item.Status = model.ContainerStatusStarting
	c.containers[id] = item
	c.started = append(c.started, id)
	return nil
}
func (c *coordinatorContainers) Create(context.Context, model.ContainerCreateParams) (uuid.UUID, error) {
	return uuid.Nil, nil
}
func (c *coordinatorContainers) Delete(context.Context, uuid.UUID) error { return nil }

type coordinatorDocker struct {
	inspections map[string]model.ContainerInspection
}

func (d *coordinatorDocker) InspectContainer(_ context.Context, dockerID string) (model.ContainerInspection, error) {
	return d.inspections[dockerID], nil
}

type coordinatorConfig struct{}

func (coordinatorConfig) Get() config.SystemConfig {
	return config.SystemConfig{ComposeCoordinatorIntervalSeconds: 1}
}
