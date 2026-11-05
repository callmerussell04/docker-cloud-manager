package compose_test

import (
	"encoding/json"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestComposeCoordinatorDoesNotTreatQueuedStartAsStarted(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	dbID := uuid.New()
	migrateID := uuid.New()
	job := startingComposeJob(t, ownerID, projectID, dbID, migrateID, map[string]bool{})
	repo := newDeploymentRepo(job)
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
	repo := newDeploymentRepo(job)
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
	repo := newDeploymentRepo(job)
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
	repo := newDeploymentRepo(job)
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
	repo := newDeploymentRepo(job)
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
