package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeReportStatsProvider struct {
	stats map[string]model.ContainerStats
}

func (f fakeReportStatsProvider) GetContainerStats(ctx context.Context, dockerID string) (model.ContainerStats, error) {
	return f.stats[dockerID], nil
}

func TestReportSnapshotRepositoryCollectsRuntimeUsage(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)

	ownerID := uuid.New()
	_, err := db.ExecContext(ctx, `
INSERT INTO containers (
    id, owner_id, docker_id, name, image_tag, status, desired_status, base_memory_reservation
) VALUES (
    $1, $2, 'docker-running', 'web', 'nginx:latest', 'running', 'running', 536870912
)`, uuid.New(), ownerID)
	require.NoError(t, err)

	repo := repository.NewReportSnapshotRepository(db, fakeReportStatsProvider{stats: map[string]model.ContainerStats{
		"docker-running": {
			MemoryUsageBytes: 268435456,
			CPUPercentage:    12.5,
		},
	}}, nil)

	now := time.Now().UTC()
	snapshots, err := repo.CollectUsageSnapshots(ctx, now.Truncate(5*time.Minute), now)
	require.NoError(t, err)
	require.Len(t, snapshots, 1)
	require.Equal(t, ownerID, snapshots[0].OwnerID)
	require.EqualValues(t, 536870912, snapshots[0].ReservedMemoryBytes)
	require.EqualValues(t, 268435456, snapshots[0].MemoryUsageBytes)
	require.Equal(t, 12.5, snapshots[0].CPUPercent)
	require.Equal(t, 1, snapshots[0].ContainersRunning)
}
