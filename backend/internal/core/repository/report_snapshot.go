package repository

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/google/uuid"
)

const bytesPerMBForReports int64 = 1024 * 1024

type ReportContainerStatsProvider interface {
	GetContainerStats(ctx context.Context, dockerID string) (model.ContainerStats, error)
}

type ReportSnapshotRepository struct {
	db           *sql.DB
	containerAPI ReportContainerStatsProvider
	logger       *slog.Logger
}

func NewReportSnapshotRepository(db *sql.DB, containerAPI ReportContainerStatsProvider, logger *slog.Logger) *ReportSnapshotRepository {
	return &ReportSnapshotRepository{
		db:           db,
		containerAPI: containerAPI,
		logger:       logging.WithComponent(logger, "report_snapshot_repository"),
	}
}

func (r *ReportSnapshotRepository) CollectUsageSnapshots(ctx context.Context, bucketStart, collectedAt time.Time) ([]model.UsageSnapshot, error) {
	const query = `
WITH owners AS (
    SELECT owner_id FROM containers
    UNION
    SELECT owner_id FROM volumes
    UNION
    SELECT owner_id FROM images
    UNION
    SELECT owner_id FROM builds
    UNION
    SELECT owner_id FROM projects
),
container_usage AS (
    SELECT
        owner_id,
        COUNT(*)::bigint AS total,
        COUNT(*) FILTER (WHERE status = 'running')::bigint AS running,
        COALESCE(SUM(base_memory_reservation) FILTER (WHERE status <> 'missing' AND desired_status = 'running'), 0)::bigint AS reserved_memory_bytes
    FROM containers
    GROUP BY owner_id
),
volume_usage AS (
    SELECT owner_id, COUNT(*)::bigint AS total, COALESCE(SUM(used_bytes), 0)::bigint AS disk_bytes
    FROM volumes
    GROUP BY owner_id
),
image_usage AS (
    SELECT owner_id, COUNT(*)::bigint AS total, COALESCE(SUM(size_mb), 0)::bigint AS disk_mb
    FROM images
    GROUP BY owner_id
),
build_usage AS (
    SELECT owner_id, COUNT(*)::bigint AS total
    FROM builds
    GROUP BY owner_id
),
project_usage AS (
    SELECT owner_id, COUNT(*)::bigint AS total
    FROM projects
    GROUP BY owner_id
)
SELECT
    owners.owner_id,
    COALESCE(container_usage.reserved_memory_bytes, 0),
    COALESCE(image_usage.disk_mb, 0) * $1::bigint,
    COALESCE(volume_usage.disk_bytes, 0),
    COALESCE(container_usage.total, 0),
    COALESCE(container_usage.running, 0),
    COALESCE(volume_usage.total, 0),
    COALESCE(image_usage.total, 0),
    COALESCE(build_usage.total, 0),
    COALESCE(project_usage.total, 0)
FROM owners
LEFT JOIN container_usage ON container_usage.owner_id = owners.owner_id
LEFT JOIN volume_usage ON volume_usage.owner_id = owners.owner_id
LEFT JOIN image_usage ON image_usage.owner_id = owners.owner_id
LEFT JOIN build_usage ON build_usage.owner_id = owners.owner_id
LEFT JOIN project_usage ON project_usage.owner_id = owners.owner_id
ORDER BY owners.owner_id`

	rows, err := r.db.QueryContext(ctx, query, bytesPerMBForReports)
	if err != nil {
		return nil, fmt.Errorf("failed to collect report usage snapshots: %w", err)
	}
	defer rows.Close()

	snapshots := make([]model.UsageSnapshot, 0)
	for rows.Next() {
		var snapshot model.UsageSnapshot
		var ownerID uuid.UUID
		if err := rows.Scan(
			&ownerID,
			&snapshot.ReservedMemoryBytes,
			&snapshot.ImageDiskBytes,
			&snapshot.VolumeDiskBytes,
			&snapshot.ContainersTotal,
			&snapshot.ContainersRunning,
			&snapshot.VolumesTotal,
			&snapshot.ImagesTotal,
			&snapshot.BuildsTotal,
			&snapshot.ProjectsTotal,
		); err != nil {
			return nil, fmt.Errorf("failed to scan report usage snapshot: %w", err)
		}
		snapshot.OwnerID = ownerID
		snapshot.TotalDiskBytes = snapshot.ImageDiskBytes + snapshot.VolumeDiskBytes
		snapshot.BucketStart = bucketStart
		snapshot.CollectedAt = collectedAt
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate report usage snapshots: %w", err)
	}
	r.applyRuntimeUsage(ctx, snapshots)
	return snapshots, nil
}

func (r *ReportSnapshotRepository) applyRuntimeUsage(ctx context.Context, snapshots []model.UsageSnapshot) {
	if r.containerAPI == nil || len(snapshots) == 0 {
		return
	}
	byOwner := make(map[uuid.UUID]*model.UsageSnapshot, len(snapshots))
	for i := range snapshots {
		byOwner[snapshots[i].OwnerID] = &snapshots[i]
	}

	rows, err := r.db.QueryContext(ctx, `
SELECT owner_id, docker_id
FROM containers
WHERE status = 'running' AND docker_id <> ''
ORDER BY owner_id`)
	if err != nil {
		r.logger.Warn("failed to list running containers for reports runtime usage", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var ownerID uuid.UUID
		var dockerID string
		if err := rows.Scan(&ownerID, &dockerID); err != nil {
			r.logger.Warn("failed to scan running container for reports runtime usage", "error", err)
			continue
		}
		snapshot, ok := byOwner[ownerID]
		if !ok {
			continue
		}
		stats, err := r.containerAPI.GetContainerStats(ctx, dockerID)
		if err != nil {
			r.logger.Warn("failed to collect container stats for reports runtime usage", "docker_id", dockerID, "error", err)
			continue
		}
		snapshot.MemoryUsageBytes += stats.MemoryUsageBytes
		snapshot.CPUPercent += stats.CPUPercentage
	}
	if err := rows.Err(); err != nil {
		r.logger.Warn("failed to iterate running containers for reports runtime usage", "error", err)
	}
}
