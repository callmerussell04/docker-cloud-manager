package repository_test

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const volumeTestMiB = int64(1 << 20)

// Volumes that existed before volume sizes get the default 256 MiB, measured
// rather than kernel-enforced.
func TestVolumeSizeMigrationGivesExistingVolumesTheDefault(t *testing.T) {
	ctx := context.Background()
	m, db := dbtest.NewCoreMigration(t)
	require.NoError(t, m.Migrate(16))
	volumeID := uuid.New()
	_, err := db.Exec(ctx, `INSERT INTO volumes (id, owner_id, name, docker_name, status) VALUES ($1, $2, 'data', 'vol_data', 'available')`, volumeID, uuid.New())
	require.NoError(t, err)

	require.NoError(t, m.Migrate(17))

	var sizeBytes int64
	var enforced bool
	require.NoError(t, db.QueryRow(ctx, `SELECT size_bytes, size_enforced FROM volumes WHERE id = $1`, volumeID).Scan(&sizeBytes, &enforced))
	require.EqualValues(t, 256<<20, sizeBytes)
	require.False(t, enforced)
}

func TestVolumeRepositoryKeepsSizeAndEnforcedFlag(t *testing.T) {
	for _, tc := range []struct {
		name     string
		size     int64
		enforced bool
	}{
		{name: "measured", size: 5 * volumeTestMiB},
		{name: "enforced", size: 1 << 30, enforced: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := dbtest.OpenCorePostgres(t)
			repo := repository.NewVolumeRepository(db)
			lifecycle := repository.NewResourceLifecycleRepository(db)
			ownerID := uuid.New()
			saved := model.Volume{ID: uuid.New(), OwnerID: ownerID, Name: "data", DockerName: "vol_data", Status: model.VolumeStatusCreating, SizeBytes: tc.size, SizeEnforced: tc.enforced}
			op := model.ResourceOperation{ID: uuid.New(), ResourceType: model.ResourceTypeVolume, ResourceID: saved.ID, OwnerID: ownerID, Operation: model.OperationCreate, Status: model.OperationStatusPending}
			outbox := model.ResourceLifecycleOutbox{ID: uuid.New(), OperationID: op.ID, ResourceType: model.ResourceTypeVolume, ResourceID: saved.ID, Exchange: "x", RoutingKey: "k", Payload: []byte(`{}`), Status: model.ResourceOutboxStatusPending}
			require.NoError(t, lifecycle.CreateQueuedVolume(ctx, saved, op, outbox))

			byID, err := repo.GetByID(ctx, saved.ID)
			require.NoError(t, err)
			require.Equal(t, tc.size, byID.SizeBytes)
			require.Equal(t, tc.enforced, byID.SizeEnforced)
			listed, _, err := repo.List(ctx, model.ListOptions{OwnerID: &ownerID})
			require.NoError(t, err)
			require.Len(t, listed, 1)
			require.Equal(t, tc.size, listed[0].SizeBytes)
			require.Equal(t, tc.enforced, listed[0].SizeEnforced)
		})
	}
}

// A created volume records whether its size is kernel-enforced, and keeps
// that flag through every later status and usage write: turning enforcement
// on or off never changes a volume that already exists.
func TestVolumeRepositoryMarkAvailableRecordsTheEnforcedFlagOnce(t *testing.T) {
	for _, enforced := range []bool{false, true} {
		t.Run(map[bool]string{false: "measured", true: "enforced"}[enforced], func(t *testing.T) {
			ctx := context.Background()
			db := dbtest.OpenCorePostgres(t)
			repo := repository.NewVolumeRepository(db)
			id := uuid.New()
			require.NoError(t, repo.Save(ctx, model.Volume{ID: id, OwnerID: uuid.New(), Name: "data", DockerName: "vol_data", Status: model.VolumeStatusCreating, SizeBytes: 8 * volumeTestMiB}))

			require.NoError(t, repo.MarkAvailable(ctx, id, enforced))
			got, err := repo.GetByID(ctx, id)
			require.NoError(t, err)
			require.Equal(t, model.VolumeStatusAvailable, got.Status)
			require.Equal(t, enforced, got.SizeEnforced)

			require.NoError(t, repo.UpdateUsage(ctx, id, 2*volumeTestMiB))
			require.NoError(t, repo.UpdateStatus(ctx, id, model.VolumeStatusMissing))
			require.NoError(t, repo.UpdateStatus(ctx, id, model.VolumeStatusAvailable))
			got, err = repo.GetByID(ctx, id)
			require.NoError(t, err)
			require.Equal(t, enforced, got.SizeEnforced)
		})
	}
	t.Run("unknown volume", func(t *testing.T) {
		repo := repository.NewVolumeRepository(dbtest.OpenCorePostgres(t))
		require.ErrorIs(t, repo.MarkAvailable(context.Background(), uuid.New(), true), apperrors.ErrNotFound)
	})
}

// A volume reserves its size, or its measurement when that is larger, until
// it is being deleted.
func TestVolumeRepositoryReservedBytes(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewVolumeRepository(db)
	ownerID, otherID := uuid.New(), uuid.New()
	save := func(owner uuid.UUID, name, status string, size, used int64) {
		t.Helper()
		id := uuid.New()
		require.NoError(t, repo.Save(ctx, model.Volume{ID: id, OwnerID: owner, Name: name, DockerName: "vol_" + id.String(), Status: status, SizeBytes: size}))
		if used > 0 {
			require.NoError(t, repo.UpdateUsage(ctx, id, used))
		}
		if status != model.VolumeStatusAvailable {
			require.NoError(t, repo.UpdateStatus(ctx, id, status))
		}
	}
	save(ownerID, "within", model.VolumeStatusAvailable, 10*volumeTestMiB, 2*volumeTestMiB)
	save(ownerID, "over", model.VolumeStatusAvailable, 4*volumeTestMiB, 7*volumeTestMiB)
	save(ownerID, "creating", model.VolumeStatusCreating, 3*volumeTestMiB, 0)
	save(ownerID, "deleting", model.VolumeStatusDeleting, 100*volumeTestMiB, 0)
	save(otherID, "other", model.VolumeStatusAvailable, 50*volumeTestMiB, 0)

	reserved, err := repo.GetUserReservedVolumeBytes(ctx, ownerID)
	require.NoError(t, err)
	require.Equal(t, (10+7+3)*volumeTestMiB, reserved)

	total, err := repo.GetTotalVolumeSizeBytes(ctx)
	require.NoError(t, err)
	require.Equal(t, (10+4+3+50)*volumeTestMiB, total, "the sum of sizes of volumes not being deleted")
}

// Only running containers that mount the volume writable are its writers.
func TestContainerRepositoryListsRunningWritersOfVolume(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	containers := repository.NewContainerRepository(db, db)
	volumes := repository.NewVolumeRepository(db)
	ownerID := uuid.New()
	volumeID, otherVolumeID := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{volumeID, otherVolumeID} {
		require.NoError(t, volumes.Save(ctx, model.Volume{ID: id, OwnerID: ownerID, Name: "v" + id.String()[:8], DockerName: "vol_" + id.String(), SizeBytes: volumeTestMiB}))
	}
	saveContainer := func(name, status string, mounts ...model.VolumeMount) uuid.UUID {
		t.Helper()
		id := uuid.New()
		require.NoError(t, containers.Save(ctx, model.Container{
			ID: id, OwnerID: ownerID, DockerID: "docker-" + id.String()[:8], Name: name, ImageTag: "nginx:latest",
			Status: status, DesiredStatus: status, BaseMemoryReservation: 128, BaseCPUReservation: 100,
		}))
		for i := range mounts {
			mounts[i].ContainerID = id
		}
		require.NoError(t, volumes.SaveMounts(ctx, mounts))
		return id
	}
	writer := saveContainer("writer", model.ContainerStatusRunning,
		model.VolumeMount{VolumeID: volumeID, MountPath: "/data"},
		model.VolumeMount{VolumeID: otherVolumeID, MountPath: "/other", IsReadOnly: true})
	saveContainer("reader", model.ContainerStatusRunning, model.VolumeMount{VolumeID: volumeID, MountPath: "/data", IsReadOnly: true})
	saveContainer("stopped", model.ContainerStatusExited, model.VolumeMount{VolumeID: volumeID, MountPath: "/data"})
	saveContainer("other volume", model.ContainerStatusRunning, model.VolumeMount{VolumeID: otherVolumeID, MountPath: "/data"})

	got, err := containers.ListRunningWritersOfVolume(ctx, volumeID)

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, writer, got[0].ID)
	require.Equal(t, ownerID, got[0].OwnerID)
	require.Equal(t, "docker-"+writer.String()[:8], got[0].DockerID)
	require.Equal(t, model.ContainerStatusRunning, got[0].Status)
}

func TestVolumeMountsAllowOneVolumePerContainerPath(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	containers := repository.NewContainerRepository(db, db)
	volumes := repository.NewVolumeRepository(db)
	ownerID, containerID := uuid.New(), uuid.New()
	first, second := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{first, second} {
		require.NoError(t, volumes.Save(ctx, model.Volume{ID: id, OwnerID: ownerID, Name: "v" + id.String()[:8], DockerName: "vol_" + id.String(), SizeBytes: volumeTestMiB}))
	}
	container := model.Container{
		ID: containerID, OwnerID: ownerID, Name: "web", ImageTag: "nginx:latest",
		Status: model.ContainerStatusPending, DesiredStatus: model.ContainerStatusCreated, BaseMemoryReservation: 128, BaseCPUReservation: 100,
	}
	operationID := uuid.New()

	err := containers.SaveWithMountsOperationAndOutbox(ctx, container,
		[]model.VolumeMount{
			{ContainerID: containerID, VolumeID: first, MountPath: "/data"},
			{ContainerID: containerID, VolumeID: second, MountPath: "/data"},
		},
		model.ResourceOperation{ID: operationID, ResourceType: model.ResourceTypeContainer, ResourceID: containerID, OwnerID: ownerID, Operation: model.OperationCreate, Status: model.OperationStatusPending},
		model.ContainerLifecycleOutbox{ID: uuid.New(), OperationID: operationID, ContainerID: containerID, Exchange: "x", RoutingKey: "k", Payload: []byte(`{}`), Status: model.ContainerOutboxStatusPending},
		false, false)

	require.Error(t, err)
	_, getErr := containers.GetByID(ctx, containerID)
	require.ErrorIs(t, getErr, apperrors.ErrNotFound)
}
