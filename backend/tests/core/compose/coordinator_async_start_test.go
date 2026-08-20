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

func TestComposeCoordinatorResolvesExternalVolumesAndCreatesManagedVolumes(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	managedID := uuid.New()
	externalID := uuid.New()
	webID := uuid.New()
	job := creatingComposeJob(t, ownerID, projectID)
	repo := newCoordinatorRepo(job)
	containers := newCoordinatorContainers(nil)
	containers.nextCreateIDs = []uuid.UUID{webID}
	volumes := &coordinatorVolumes{
		createIDs: map[string]uuid.UUID{"wh_data": managedID},
		resolveIDs: map[string]uuid.UUID{
			"shared-cache": externalID,
		},
	}

	runCoordinatorOnceWithVolumes(t, repo, volumes, containers, &coordinatorDocker{})

	require.Len(t, volumes.created, 1)
	require.Equal(t, "wh_data", volumes.created[0].Name)
	require.Equal(t, projectID, *volumes.created[0].ProjectID)
	require.Equal(t, []string{"shared-cache"}, volumes.resolved)
	require.Len(t, containers.created, 1)
	require.ElementsMatch(t, []model.VolumeMountParams{
		{VolumeID: managedID, MountPath: "/data"},
		{VolumeID: externalID, MountPath: "/cache", IsReadOnly: true},
	}, containers.created[0].VolumeMounts)

	var resources struct {
		VolumeIDs            map[string]string `json:"volume_ids"`
		ManagedVolumeAliases map[string]bool   `json:"managed_volume_aliases"`
	}
	require.NotEmpty(t, repo.savedResourceMaps)
	require.NoError(t, json.Unmarshal(repo.savedResourceMaps[len(repo.savedResourceMaps)-1], &resources))
	require.Equal(t, managedID.String(), resources.VolumeIDs["data"])
	require.Equal(t, externalID.String(), resources.VolumeIDs["cache"])
	require.True(t, resources.ManagedVolumeAliases["data"])
	require.False(t, resources.ManagedVolumeAliases["cache"])
}

func TestComposeCoordinatorReusesExistingManagedVolume(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	managedID := uuid.New()
	webID := uuid.New()
	job := creatingComposeJob(t, ownerID, projectID)
	repo := newCoordinatorRepo(job)
	containers := newCoordinatorContainers(nil)
	containers.nextCreateIDs = []uuid.UUID{webID}
	volumes := &coordinatorVolumes{
		managedIDs: map[string]uuid.UUID{"wh_data": managedID},
		resolveIDs: map[string]uuid.UUID{"shared-cache": uuid.New()},
	}

	runCoordinatorOnceWithVolumes(t, repo, volumes, containers, &coordinatorDocker{})

	require.Empty(t, volumes.created)
	require.Equal(t, []string{"wh_data"}, volumes.managedResolved)
	require.Len(t, containers.created, 1)
	require.Contains(t, containers.created[0].VolumeMounts, model.VolumeMountParams{VolumeID: managedID, MountPath: "/data"})

	var resources struct {
		ManagedVolumeAliases map[string]bool `json:"managed_volume_aliases"`
	}
	require.NotEmpty(t, repo.savedResourceMaps)
	require.NoError(t, json.Unmarshal(repo.savedResourceMaps[len(repo.savedResourceMaps)-1], &resources))
	require.False(t, resources.ManagedVolumeAliases["data"])
}

func TestComposeCoordinatorRollbackUnlinksReusedManagedVolume(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	managedID := uuid.New()
	externalID := uuid.New()
	job := rollingBackComposeJob(t, ownerID, projectID, managedID, externalID)
	repo := newCoordinatorRepo(job)
	volumes := &coordinatorVolumes{}

	runCoordinatorOnceWithVolumes(t, repo, volumes, newCoordinatorContainers(nil), &coordinatorDocker{})

	require.Empty(t, volumes.deleted)
	require.Equal(t, []uuid.UUID{managedID}, volumes.unlinked)
	require.Equal(t, model.ComposeDeploymentStatusFailed, repo.completedStatus)
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

func creatingComposeJob(t *testing.T, ownerID, projectID uuid.UUID) model.ComposeDeploymentJob {
	t.Helper()
	plan := map[string]any{
		"project_name": "wh",
		"source_type":  model.ComposeSourceTypeGit,
		"volumes": []model.ComposeVolume{
			{Alias: "data", Name: "wh_data"},
			{Alias: "cache", Name: "shared-cache", External: true},
		},
		"services": []model.ComposeService{
			{
				Name:     "web",
				ImageTag: "nginx:latest",
				VolumeMounts: []model.VolumeMountParams{
					{VolumeName: "data", MountPath: "/data"},
					{VolumeName: "cache", MountPath: "/cache", IsReadOnly: true},
				},
			},
		},
	}
	planJSON, err := json.Marshal(plan)
	require.NoError(t, err)
	resourceJSON, err := json.Marshal(map[string]any{})
	require.NoError(t, err)
	return model.ComposeDeploymentJob{
		ID:              uuid.New(),
		ProjectID:       projectID,
		OwnerID:         ownerID,
		Status:          model.ComposeDeploymentStatusRunning,
		Stage:           model.ComposeDeploymentStageCreating,
		PlanJSON:        planJSON,
		ResourceMapJSON: resourceJSON,
	}
}

func rollingBackComposeJob(t *testing.T, ownerID, projectID, managedID, externalID uuid.UUID) model.ComposeDeploymentJob {
	t.Helper()
	plan := map[string]any{
		"project_name": "wh",
		"source_type":  model.ComposeSourceTypeGit,
		"volumes": []model.ComposeVolume{
			{Alias: "data", Name: "wh_data"},
			{Alias: "cache", Name: "shared-cache", External: true},
		},
		"services": []model.ComposeService{},
	}
	resources := map[string]any{
		"volume_ids": map[string]string{
			"data":  managedID.String(),
			"cache": externalID.String(),
		},
		"managed_volume_aliases": map[string]bool{
			"data":  false,
			"cache": false,
		},
		"cleanup_status": model.ComposeDeploymentStatusFailed,
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
		Stage:           model.ComposeDeploymentStageRollingBack,
		PlanJSON:        planJSON,
		ResourceMapJSON: resourceJSON,
	}
}

func runCoordinatorOnce(t *testing.T, repo *coordinatorRepo, containers *coordinatorContainers, docker *coordinatorDocker) {
	t.Helper()
	runCoordinatorOnceWithVolumes(t, repo, nil, containers, docker)
}

func runCoordinatorOnceWithVolumes(t *testing.T, repo *coordinatorRepo, volumes *coordinatorVolumes, containers *coordinatorContainers, docker *coordinatorDocker) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	repo.cancel = cancel
	orch := compose.NewOrchestrator(ctx, repo, nil, volumes, containers, docker, coordinatorConfig{}, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	mu            sync.Mutex
	containers    map[uuid.UUID]model.Container
	started       []uuid.UUID
	created       []model.ContainerCreateParams
	nextCreateIDs []uuid.UUID
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
func (c *coordinatorContainers) Create(_ context.Context, params model.ContainerCreateParams) (uuid.UUID, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := uuid.New()
	if len(c.nextCreateIDs) > 0 {
		id = c.nextCreateIDs[0]
		c.nextCreateIDs = c.nextCreateIDs[1:]
	}
	if c.containers == nil {
		c.containers = make(map[uuid.UUID]model.Container)
	}
	c.created = append(c.created, params)
	c.containers[id] = model.Container{ID: id, Status: model.ContainerStatusCreated}
	return id, nil
}
func (c *coordinatorContainers) Delete(context.Context, uuid.UUID) error { return nil }

type coordinatorVolumes struct {
	createIDs       map[string]uuid.UUID
	managedIDs      map[string]uuid.UUID
	resolveIDs      map[string]uuid.UUID
	created         []model.VolumeCreateParams
	resolved        []string
	managedResolved []string
	deleted         []uuid.UUID
	unlinked        []uuid.UUID
}

func (v *coordinatorVolumes) Create(_ context.Context, params model.VolumeCreateParams) (uuid.UUID, error) {
	v.created = append(v.created, params)
	if id, ok := v.createIDs[params.Name]; ok {
		return id, nil
	}
	return uuid.New(), nil
}

func (v *coordinatorVolumes) ResolveProjectManagedByName(ctx context.Context, projectID uuid.UUID, name string) (uuid.UUID, bool, error) {
	v.managedResolved = append(v.managedResolved, name)
	if id, ok := v.managedIDs[name]; ok {
		return id, true, nil
	}
	id, err := v.Create(ctx, model.VolumeCreateParams{ProjectID: &projectID, Name: name})
	return id, false, err
}

func (v *coordinatorVolumes) ResolveByName(_ context.Context, name string) (uuid.UUID, error) {
	v.resolved = append(v.resolved, name)
	if id, ok := v.resolveIDs[name]; ok {
		return id, nil
	}
	return uuid.Nil, nil
}

func (v *coordinatorVolumes) Delete(_ context.Context, volumeID uuid.UUID) error {
	v.deleted = append(v.deleted, volumeID)
	return nil
}

func (v *coordinatorVolumes) UnlinkProjectVolume(_ context.Context, _ uuid.UUID, volumeID uuid.UUID) error {
	v.unlinked = append(v.unlinked, volumeID)
	return nil
}

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
