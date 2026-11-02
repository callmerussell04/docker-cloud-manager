package compose_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type persistedResources struct {
	BuildIDs             map[string]uuid.UUID `json:"build_ids"`
	VolumeIDs            map[string]uuid.UUID `json:"volume_ids"`
	ManagedVolumeAliases map[string]bool      `json:"managed_volume_aliases"`
	ContainerIDs         map[string]uuid.UUID `json:"container_ids"`
	CleanupStatus        string               `json:"cleanup_status"`
	CleanupError         string               `json:"cleanup_error"`
}

func buildingPlan() map[string]any {
	return map[string]any{
		"project_name": "wh",
		"source_type":  model.ComposeSourceTypeUpload,
		"services": []model.ComposeService{
			{Name: "api", ImageTag: "wh_api:latest", BuildContext: "api"},
		},
	}
}

func webPlan(volumes []model.ComposeVolume, mounts []model.VolumeMountParams) map[string]any {
	return map[string]any{
		"project_name": "wh",
		"source_type":  model.ComposeSourceTypeUpload,
		"volumes":      volumes,
		"services": []model.ComposeService{
			{Name: "web", ImageTag: "nginx:latest", VolumeMounts: mounts},
		},
	}
}

func TestComposeCoordinatorBuildingWaitsForPendingBuilds(t *testing.T) {
	buildID := uuid.New()
	job := persistedComposeJob(t, uuid.New(), uuid.New(), model.ComposeDeploymentStageBuilding, buildingPlan(),
		map[string]any{"build_ids": map[string]uuid.UUID{"api": buildID}})
	env := newCoordinatorEnv(job)
	env.builds.builds = map[uuid.UUID]model.Build{buildID: {ID: buildID, Status: model.BuildStatusRunning}}

	env.runOnce(t)

	progress := env.repo.lastProgress(t)
	require.Equal(t, model.ComposeDeploymentStageBuilding, progress.stage)
	require.Equal(t, model.ProjectStatusBuilding, progress.projectStatus)
	require.Empty(t, env.sources.released)
	require.Empty(t, env.builds.triggered)
	require.Empty(t, env.repo.completedStatus)
}

func TestComposeCoordinatorBuildingResumesPersistedBuildsWithoutRetrigger(t *testing.T) {
	buildID := uuid.New()
	job := persistedComposeJob(t, uuid.New(), uuid.New(), model.ComposeDeploymentStageBuilding, buildingPlan(), map[string]any{})
	env := newCoordinatorEnv(job)
	env.builds.project = []model.Build{{ID: buildID, ProjectServiceName: "api", Status: model.BuildStatusPending}}
	env.builds.builds = map[uuid.UUID]model.Build{buildID: {ID: buildID, Status: model.BuildStatusPending}}

	env.runOnce(t)

	require.Empty(t, env.builds.triggered)
	var resources persistedResources
	env.repo.lastResources(t, &resources)
	require.Equal(t, map[string]uuid.UUID{"api": buildID}, resources.BuildIDs)
}

func TestComposeCoordinatorBuildingAdvancesToCreatingAfterSuccess(t *testing.T) {
	buildID := uuid.New()
	job := persistedComposeJob(t, uuid.New(), uuid.New(), model.ComposeDeploymentStageBuilding, buildingPlan(),
		map[string]any{"build_ids": map[string]uuid.UUID{"api": buildID}})
	env := newCoordinatorEnv(job)
	env.builds.builds = map[uuid.UUID]model.Build{buildID: {ID: buildID, Status: model.BuildStatusSuccess}}

	env.runOnce(t)

	progress := env.repo.lastProgress(t)
	require.Equal(t, model.ComposeDeploymentStageCreating, progress.stage)
	require.Equal(t, model.ProjectStatusDeploying, progress.projectStatus)
	require.Equal(t, []string{job.SourceObjectKey}, env.sources.released)
	require.Empty(t, env.repo.completedStatus)
}

func TestComposeCoordinatorBuildingFailureCancelsBuildsAndRollsBack(t *testing.T) {
	buildID := uuid.New()
	job := persistedComposeJob(t, uuid.New(), uuid.New(), model.ComposeDeploymentStageBuilding, buildingPlan(),
		map[string]any{"build_ids": map[string]uuid.UUID{"api": buildID}})
	env := newCoordinatorEnv(job)
	env.builds.builds = map[uuid.UUID]model.Build{buildID: {ID: buildID, Status: model.BuildStatusFailed}}

	env.runOnce(t)

	require.Equal(t, []uuid.UUID{buildID}, env.builds.canceled)
	require.Equal(t, model.ComposeDeploymentStageRollingBack, env.repo.progress[len(env.repo.progress)-1].stage)
	require.Equal(t, model.ComposeDeploymentStatusFailed, env.repo.completedStatus)
	require.Equal(t, "build for service api failed with status: failed", env.repo.completedMessage)
	require.Equal(t, model.ProjectStatusFailed, env.repo.projectStatuses[len(env.repo.projectStatuses)-1])
	require.Equal(t, []string{job.SourceObjectKey}, env.sources.released)
	require.Empty(t, env.images.deleted)
}

func TestComposeCoordinatorBuildingCanceledBuildCancelsDeployment(t *testing.T) {
	buildID := uuid.New()
	job := persistedComposeJob(t, uuid.New(), uuid.New(), model.ComposeDeploymentStageBuilding, buildingPlan(),
		map[string]any{"build_ids": map[string]uuid.UUID{"api": buildID}})
	env := newCoordinatorEnv(job)
	env.builds.builds = map[uuid.UUID]model.Build{buildID: {ID: buildID, Status: model.BuildStatusCanceled}}

	env.runOnce(t)

	require.Empty(t, env.builds.canceled)
	require.Equal(t, model.ComposeDeploymentStatusCanceled, env.repo.completedStatus)
	require.Equal(t, "compose deployment canceled", env.repo.completedMessage)
	require.Equal(t, model.ProjectStatusCanceled, env.repo.projectStatuses[len(env.repo.projectStatuses)-1])
	require.Contains(t, env.sources.released, job.SourceObjectKey)
}

func TestComposeCoordinatorCreatingWaitsForManagedVolume(t *testing.T) {
	managedID := uuid.New()
	job := persistedComposeJob(t, uuid.New(), uuid.New(), model.ComposeDeploymentStageCreating,
		webPlan([]model.ComposeVolume{{Alias: "data", Name: "wh_data"}}, []model.VolumeMountParams{{VolumeName: "data", MountPath: "/data"}}),
		map[string]any{})
	env := newCoordinatorEnv(job)
	env.volumes.createIDs = map[string]uuid.UUID{"wh_data": managedID}
	env.volumes.statuses = map[uuid.UUID]string{managedID: model.VolumeStatusCreating}

	env.runOnce(t)

	require.Empty(t, env.containers.created)
	progress := env.repo.lastProgress(t)
	require.Equal(t, model.ComposeDeploymentStageCreating, progress.stage)
	var resources persistedResources
	env.repo.lastResources(t, &resources)
	require.Equal(t, managedID, resources.VolumeIDs["data"])
	require.True(t, resources.ManagedVolumeAliases["data"])
}

func TestComposeCoordinatorCreatingFailsOnErroredManagedVolume(t *testing.T) {
	managedID := uuid.New()
	job := persistedComposeJob(t, uuid.New(), uuid.New(), model.ComposeDeploymentStageCreating,
		webPlan([]model.ComposeVolume{{Alias: "data", Name: "wh_data"}}, []model.VolumeMountParams{{VolumeName: "data", MountPath: "/data"}}),
		map[string]any{})
	env := newCoordinatorEnv(job)
	env.volumes.createIDs = map[string]uuid.UUID{"wh_data": managedID}
	env.volumes.statuses = map[uuid.UUID]string{managedID: model.VolumeStatusError}

	env.runOnce(t)

	require.Empty(t, env.containers.created)
	require.Equal(t, []uuid.UUID{managedID}, env.volumes.deleted)
	require.Equal(t, model.ComposeDeploymentStageRollingBack, env.repo.lastProgress(t).stage)
	require.Empty(t, env.repo.completedStatus, "rollback waits for the queued volume delete")
}

func TestComposeCoordinatorCreatingResumesExistingContainers(t *testing.T) {
	ownerID := uuid.New()
	webID := uuid.New()
	job := persistedComposeJob(t, ownerID, uuid.New(), model.ComposeDeploymentStageCreating, webPlan(nil, nil), map[string]any{})
	env := newCoordinatorEnv(job)
	env.resources.containers = []model.Container{{ID: webID, Name: "wh_web"}}
	env.containers = newCoordinatorContainers(map[uuid.UUID]model.Container{
		webID: {ID: webID, OwnerID: ownerID, Status: model.ContainerStatusCreated},
	})

	env.runOnce(t)

	require.Empty(t, env.containers.created)
	require.Equal(t, 1, env.repo.serviceGraphs)
	progress := env.repo.lastProgress(t)
	require.Equal(t, model.ComposeDeploymentStageStarting, progress.stage)
	require.Equal(t, model.ProjectStatusDeploying, progress.projectStatus)
	var resources persistedResources
	env.repo.lastResources(t, &resources)
	require.Equal(t, map[string]uuid.UUID{"web": webID}, resources.ContainerIDs)
}

func TestComposeCoordinatorCreatesContainersWithJobScopeAndRequestID(t *testing.T) {
	ownerID := uuid.New()
	job := persistedComposeJob(t, ownerID, uuid.New(), model.ComposeDeploymentStageCreating, webPlan(nil, nil), map[string]any{})
	env := newCoordinatorEnv(job)

	env.runOnce(t)

	require.Len(t, env.containers.created, 1)
	require.Equal(t, "wh_web", env.containers.created[0].Name)
	require.Equal(t, job.ProjectID, *env.containers.created[0].ProjectID)
	require.Equal(t, accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID}, env.containers.createScopes[0])
	require.Equal(t, job.RequestID, env.containers.createRequestIDs[0])
}

func TestComposeCoordinatorStartingCompletesDeployment(t *testing.T) {
	ownerID := uuid.New()
	dbID := uuid.New()
	migrateID := uuid.New()
	exitCode := 0
	job := startingComposeJob(t, ownerID, uuid.New(), dbID, migrateID, map[string]bool{"db": true})
	job.SourceObjectKey = "compose-sources/done.zip"
	env := newCoordinatorEnv(job)
	env.containers = newCoordinatorContainers(map[uuid.UUID]model.Container{
		dbID:      {ID: dbID, Status: model.ContainerStatusRunning},
		migrateID: {ID: migrateID, Status: model.ContainerStatusExited, LastExitCode: &exitCode},
	})

	env.runOnce(t)

	require.Empty(t, env.containers.started)
	require.Equal(t, model.ProjectStatusRunning, env.repo.projectStatuses[len(env.repo.projectStatuses)-1])
	require.Equal(t, model.ComposeDeploymentStatusSucceeded, env.repo.completedStatus)
	require.Equal(t, []string{job.SourceObjectKey}, env.sources.released)
}

func TestComposeCoordinatorCancelRollsBackOnlyDeploymentResources(t *testing.T) {
	webID := uuid.New()
	managedID := uuid.New()
	externalID := uuid.New()
	job := persistedComposeJob(t, uuid.New(), uuid.New(), model.ComposeDeploymentStageCreating,
		webPlan([]model.ComposeVolume{{Alias: "data", Name: "wh_data"}, {Alias: "cache", Name: "shared", External: true}}, nil),
		map[string]any{
			"container_ids":          map[string]uuid.UUID{"web": webID},
			"volume_ids":             map[string]uuid.UUID{"data": managedID, "cache": externalID},
			"managed_volume_aliases": map[string]bool{"data": true, "cache": false},
		})
	job.CancelRequested = true
	env := newCoordinatorEnv(job)

	env.runOnce(t)

	require.Equal(t, []uuid.UUID{webID}, env.containers.deleted)
	require.Equal(t, []uuid.UUID{managedID}, env.volumes.deleted)
	require.Empty(t, env.volumes.unlinked)
	require.Equal(t, []string{job.SourceObjectKey}, env.sources.released)
	require.Empty(t, env.repo.completedStatus, "queued deletes keep the job rolling back")
	progress := env.repo.lastProgress(t)
	require.Equal(t, model.ComposeDeploymentStageRollingBack, progress.stage)
	require.Equal(t, model.ProjectStatusCanceled, progress.projectStatus)
	var resources persistedResources
	env.repo.lastResources(t, &resources)
	require.Equal(t, model.ComposeDeploymentStatusCanceled, resources.CleanupStatus)
}

func rollingBackWebJob(t *testing.T, webID, managedID, buildID uuid.UUID) model.ComposeDeploymentJob {
	t.Helper()
	return persistedComposeJob(t, uuid.New(), uuid.New(), model.ComposeDeploymentStageRollingBack,
		webPlan([]model.ComposeVolume{{Alias: "data", Name: "wh_data"}}, nil),
		map[string]any{
			"build_ids":              map[string]uuid.UUID{"web": buildID},
			"container_ids":          map[string]uuid.UUID{"web": webID},
			"volume_ids":             map[string]uuid.UUID{"data": managedID},
			"managed_volume_aliases": map[string]bool{"data": true},
			"cleanup_status":         model.ComposeDeploymentStatusFailed,
			"cleanup_error":          "service web failed",
		})
}

func TestComposeCoordinatorRollbackCompletesWhenResourcesAreGone(t *testing.T) {
	webID := uuid.New()
	managedID := uuid.New()
	buildID := uuid.New()
	imageID := uuid.New()
	job := rollingBackWebJob(t, webID, managedID, buildID)
	env := newCoordinatorEnv(job)
	env.containers.deleteErrs = map[uuid.UUID]error{webID: apperrors.ErrNotFound}
	env.volumes.deleteErrs = map[uuid.UUID]error{managedID: apperrors.ErrNotFound}
	env.builds.builds = map[uuid.UUID]model.Build{buildID: {ID: buildID, ImageID: imageID, Status: model.BuildStatusSuccess}}

	env.runOnce(t)

	require.Equal(t, []uuid.UUID{imageID}, env.images.deleted)
	require.Equal(t, model.ComposeDeploymentStatusFailed, env.repo.completedStatus)
	require.Equal(t, "service web failed", env.repo.completedMessage)
	require.Equal(t, []string{model.ProjectStatusFailed}, env.repo.projectStatuses)
	require.Equal(t, []string{job.SourceObjectKey}, env.sources.released)
}

func TestComposeCoordinatorRollbackWaitsOnConflictingDelete(t *testing.T) {
	webID := uuid.New()
	managedID := uuid.New()
	buildID := uuid.New()
	job := rollingBackWebJob(t, webID, managedID, buildID)
	env := newCoordinatorEnv(job)
	env.containers.deleteErrs = map[uuid.UUID]error{webID: apperrors.ErrConflict}
	env.volumes.deleteErrs = map[uuid.UUID]error{managedID: apperrors.ErrNotFound}

	env.runOnce(t)

	require.Empty(t, env.repo.completedStatus)
	require.Empty(t, env.images.deleted)
	require.Empty(t, env.sources.released)
	progress := env.repo.lastProgress(t)
	require.Equal(t, model.ComposeDeploymentStageRollingBack, progress.stage)
	require.Equal(t, model.ProjectStatusFailed, progress.projectStatus)
}

func TestComposeCoordinatorCancelDuringBuildingLeavesBuildRecordsAlone(t *testing.T) {
	buildID := uuid.New()
	job := persistedComposeJob(t, uuid.New(), uuid.New(), model.ComposeDeploymentStageBuilding, buildingPlan(),
		map[string]any{"build_ids": map[string]uuid.UUID{"api": buildID}})
	job.Status = model.ComposeDeploymentStatusCanceling
	env := newCoordinatorEnv(job)
	env.builds.builds = map[uuid.UUID]model.Build{buildID: {ID: buildID, Status: model.BuildStatusRunning}}

	env.runOnce(t)

	require.Empty(t, env.builds.canceled)
	require.Equal(t, model.ComposeDeploymentStatusCanceled, env.repo.completedStatus)
	require.Equal(t, "compose deployment canceled", env.repo.completedMessage)
	require.Equal(t, []string{model.ProjectStatusCanceled}, env.repo.projectStatuses)
	require.Contains(t, env.sources.released, job.SourceObjectKey)
}

func TestComposeCoordinatorCancelDuringStartingRollsBackContainers(t *testing.T) {
	dbID := uuid.New()
	migrateID := uuid.New()
	job := startingComposeJob(t, uuid.New(), uuid.New(), dbID, migrateID, map[string]bool{"db": true})
	job.CancelRequested = true
	job.SourceObjectKey = "compose-sources/starting.zip"
	env := newCoordinatorEnv(job)

	env.runOnce(t)

	require.Empty(t, env.containers.started)
	require.ElementsMatch(t, []uuid.UUID{dbID, migrateID}, env.containers.deleted)
	require.Empty(t, env.repo.completedStatus, "queued deletes keep the job rolling back")
	progress := env.repo.lastProgress(t)
	require.Equal(t, model.ComposeDeploymentStageRollingBack, progress.stage)
	require.Equal(t, model.ProjectStatusCanceled, progress.projectStatus)
	require.Equal(t, []string{job.SourceObjectKey}, env.sources.released)
}

func TestComposeCoordinatorStartingFailureRollsBack(t *testing.T) {
	dbID := uuid.New()
	migrateID := uuid.New()
	lastError := "image pull failed"
	job := startingComposeJob(t, uuid.New(), uuid.New(), dbID, migrateID, map[string]bool{"db": true})
	env := newCoordinatorEnv(job)
	env.containers = newCoordinatorContainers(map[uuid.UUID]model.Container{
		dbID:      {ID: dbID, Status: model.ContainerStatusRunning},
		migrateID: {ID: migrateID, Status: model.ContainerStatusError, LastError: &lastError},
	})
	env.containers.deleteErrs = map[uuid.UUID]error{dbID: apperrors.ErrNotFound, migrateID: apperrors.ErrNotFound}

	env.runOnce(t)

	require.Empty(t, env.containers.started)
	require.ElementsMatch(t, []uuid.UUID{dbID, migrateID}, env.containers.deleted)
	require.Equal(t, model.ComposeDeploymentStatusFailed, env.repo.completedStatus)
	require.Equal(t, "service migrate failed: image pull failed", env.repo.completedMessage)
	require.Equal(t, model.ProjectStatusFailed, env.repo.projectStatuses[len(env.repo.projectStatuses)-1])
}
