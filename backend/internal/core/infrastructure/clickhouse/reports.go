package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/google/uuid"
)

type Config struct {
	Addr     string
	Database string
	Username string
	Password string
}

type ReportsRepository struct {
	cfg Config
	mu  sync.Mutex
	db  *sql.DB
}

func NewReportsRepository(cfg Config) *ReportsRepository {
	return &ReportsRepository{cfg: cfg}
}

func (r *ReportsRepository) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *ReportsRepository) dbConn() (*sql.DB, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.db != nil {
		return r.db, nil
	}
	if strings.TrimSpace(r.cfg.Addr) == "" {
		return nil, fmt.Errorf("clickhouse address is not configured")
	}

	u := url.URL{
		Scheme: "clickhouse",
		Host:   r.cfg.Addr,
		Path:   "/" + strings.TrimLeft(r.cfg.Database, "/"),
	}
	q := u.Query()
	q.Set("dial_timeout", "5s")
	q.Set("compress", "lz4")
	if r.cfg.Username != "" {
		q.Set("username", r.cfg.Username)
	}
	if r.cfg.Password != "" {
		q.Set("password", r.cfg.Password)
	}
	u.RawQuery = q.Encode()

	db, err := sql.Open("clickhouse", u.String())
	if err != nil {
		return nil, fmt.Errorf("failed to open clickhouse connection: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping clickhouse: %w", err)
	}
	r.db = db
	return db, nil
}

func (r *ReportsRepository) RecordAuditEvent(ctx context.Context, event model.AuditEvent) error {
	db, err := r.dbConn()
	if err != nil {
		return err
	}
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	_, err = db.ExecContext(ctx, `
INSERT INTO audit_events (
    id, occurred_at, actor_user_id, actor_username, actor_scope, action, outcome,
    resource_type, resource_id, resource_name, owner_id, owner_username,
    request_id, client_ip, user_agent, error_code, details_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID,
		event.OccurredAt.UTC(),
		nullableUUID(event.ActorUserID),
		event.ActorUsername,
		event.ActorScope,
		event.Action,
		event.Outcome,
		event.ResourceType,
		event.ResourceID,
		event.ResourceName,
		nullableUUID(event.OwnerID),
		event.OwnerUsername,
		event.RequestID,
		event.ClientIP,
		event.UserAgent,
		event.ErrorCode,
		event.DetailsJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to record audit event: %w", err)
	}
	return nil
}

func (r *ReportsRepository) InsertUsageSnapshots(ctx context.Context, snapshots []model.UsageSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}
	db, err := r.dbConn()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin usage snapshot insert: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO user_resource_usage_snapshots (
    owner_id, owner_username, bucket_start, collected_at, memory_usage_bytes,
    reserved_memory_bytes, cpu_percent, image_disk_bytes, volume_disk_bytes, total_disk_bytes, containers_total,
    containers_running, volumes_total, images_total, builds_total, projects_total
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare usage snapshot insert: %w", err)
	}
	defer stmt.Close()

	for _, snapshot := range snapshots {
		if _, err := stmt.ExecContext(ctx,
			snapshot.OwnerID,
			snapshot.OwnerUsername,
			snapshot.BucketStart.UTC(),
			snapshot.CollectedAt.UTC(),
			snapshot.MemoryUsageBytes,
			snapshot.ReservedMemoryBytes,
			snapshot.CPUPercent,
			snapshot.ImageDiskBytes,
			snapshot.VolumeDiskBytes,
			snapshot.TotalDiskBytes,
			snapshot.ContainersTotal,
			snapshot.ContainersRunning,
			snapshot.VolumesTotal,
			snapshot.ImagesTotal,
			snapshot.BuildsTotal,
			snapshot.ProjectsTotal,
		); err != nil {
			return fmt.Errorf("failed to insert usage snapshot: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit usage snapshot insert: %w", err)
	}
	committed = true
	return nil
}

func (r *ReportsRepository) GetReportsOverview(ctx context.Context, from, to time.Time) (model.ReportsOverview, error) {
	db, err := r.dbConn()
	if err != nil {
		return model.ReportsOverview{}, err
	}
	overview := model.ReportsOverview{From: from, To: to}
	if err := db.QueryRowContext(ctx, `
SELECT count(), countIf(outcome = 'failure'), uniqExactIf(actor_user_id, actor_user_id != toUUID('00000000-0000-0000-0000-000000000000'))
FROM audit_events
WHERE occurred_at >= ? AND occurred_at < ?`, from.UTC(), to.UTC()).Scan(
		&overview.AuditEventsTotal,
		&overview.FailedActionsTotal,
		&overview.ActiveUsersTotal,
	); err != nil {
		return model.ReportsOverview{}, fmt.Errorf("failed to query reports overview events: %w", err)
	}
	if err := db.QueryRowContext(ctx, `
SELECT
    ifNull(sum(memory_usage_bytes), 0),
    ifNull(sum(reserved_memory_bytes), 0),
    ifNull(sum(cpu_percent), 0),
    ifNull(sum(total_disk_bytes), 0),
    ifNull(sum(containers_total + volumes_total + images_total + builds_total + projects_total), 0),
    ifNull(sum(containers_total), 0),
    ifNull(sum(containers_running), 0),
    ifNull(sum(volumes_total), 0),
    ifNull(sum(images_total), 0),
    ifNull(sum(builds_total), 0),
    ifNull(sum(projects_total), 0)
FROM (
    SELECT
        owner_id,
        argMax(memory_usage_bytes, collected_at) AS memory_usage_bytes,
        argMax(reserved_memory_bytes, collected_at) AS reserved_memory_bytes,
        argMax(cpu_percent, collected_at) AS cpu_percent,
        argMax(total_disk_bytes, collected_at) AS total_disk_bytes,
        argMax(containers_total, collected_at) AS containers_total,
        argMax(containers_running, collected_at) AS containers_running,
        argMax(volumes_total, collected_at) AS volumes_total,
        argMax(images_total, collected_at) AS images_total,
        argMax(builds_total, collected_at) AS builds_total,
        argMax(projects_total, collected_at) AS projects_total
    FROM user_resource_usage_snapshots
    WHERE bucket_start >= ? AND bucket_start < ?
    GROUP BY owner_id
)`, from.UTC(), to.UTC()).Scan(
		&overview.MemoryUsageBytes,
		&overview.ReservedMemoryBytes,
		&overview.CPUPercent,
		&overview.TotalDiskBytes,
		&overview.ResourcesTotal,
		&overview.ContainersTotal,
		&overview.ContainersRunning,
		&overview.VolumesTotal,
		&overview.ImagesTotal,
		&overview.BuildsTotal,
		&overview.ProjectsTotal,
	); err != nil {
		return model.ReportsOverview{}, fmt.Errorf("failed to query reports overview usage: %w", err)
	}
	return overview, nil
}

func (r *ReportsRepository) GetLatestUsageSnapshotCollectedAt(ctx context.Context) (*time.Time, error) {
	db, err := r.dbConn()
	if err != nil {
		return nil, err
	}
	var collectedAt time.Time
	if err := db.QueryRowContext(ctx, `
SELECT ifNull(max(collected_at), toDateTime64(0, 3, 'UTC'))
FROM user_resource_usage_snapshots`).Scan(&collectedAt); err != nil {
		return nil, fmt.Errorf("failed to query latest usage snapshot timestamp: %w", err)
	}
	if collectedAt.IsZero() || collectedAt.Unix() == 0 {
		return nil, nil
	}
	return &collectedAt, nil
}

func (r *ReportsRepository) ListUserUsageReport(ctx context.Context, from, to time.Time, sort, search string, limit, offset int) ([]model.UserUsageReportItem, int, error) {
	db, err := r.dbConn()
	if err != nil {
		return nil, 0, err
	}
	orderBy := "total_disk_bytes DESC"
	switch sort {
	case "actual_memory":
		orderBy = "memory_usage_bytes DESC"
	case "reserved_memory", "memory":
		orderBy = "reserved_memory_bytes DESC"
	case "cpu":
		orderBy = "cpu_percent DESC"
	case "disk":
		orderBy = "total_disk_bytes DESC"
	case "resources":
		orderBy = "resources_total DESC"
	case "actions":
		orderBy = "actions_total DESC"
	case "containers":
		orderBy = "containers_total DESC"
	}
	searchWhere, searchArgs := userUsageSearchWhere(search)

	var total int
	countArgs := []any{from.UTC(), to.UTC()}
	countArgs = append(countArgs, searchArgs...)
	if err := db.QueryRowContext(ctx, `
SELECT count()
FROM (
    SELECT
        owner_id,
        argMax(owner_username, collected_at) AS owner_username
    FROM user_resource_usage_snapshots
    WHERE bucket_start >= ? AND bucket_start < ?
    GROUP BY owner_id
) AS u
`+searchWhere, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count user usage report: %w", err)
	}

	query := fmt.Sprintf(`
SELECT
    u.owner_id,
    u.owner_username,
    u.memory_usage_bytes,
    u.reserved_memory_bytes,
    u.cpu_percent,
    u.total_disk_bytes,
    u.containers_total + u.volumes_total + u.images_total + u.builds_total + u.projects_total AS resources_total,
    u.containers_total,
    u.containers_running,
    u.volumes_total,
    u.images_total,
    u.builds_total,
    u.projects_total,
    ifNull(a.actions_total, 0) AS actions_total
FROM (
    SELECT
        owner_id,
        argMax(owner_username, collected_at) AS owner_username,
        argMax(memory_usage_bytes, collected_at) AS memory_usage_bytes,
        argMax(reserved_memory_bytes, collected_at) AS reserved_memory_bytes,
        argMax(cpu_percent, collected_at) AS cpu_percent,
        argMax(total_disk_bytes, collected_at) AS total_disk_bytes,
        argMax(containers_total, collected_at) AS containers_total,
        argMax(containers_running, collected_at) AS containers_running,
        argMax(volumes_total, collected_at) AS volumes_total,
        argMax(images_total, collected_at) AS images_total,
        argMax(builds_total, collected_at) AS builds_total,
        argMax(projects_total, collected_at) AS projects_total
    FROM user_resource_usage_snapshots
    WHERE bucket_start >= ? AND bucket_start < ?
    GROUP BY owner_id
) AS u
LEFT JOIN (
    SELECT assumeNotNull(actor_user_id) AS owner_id, count() AS actions_total
    FROM audit_events
    WHERE occurred_at >= ? AND occurred_at < ? AND actor_user_id != toUUID('00000000-0000-0000-0000-000000000000')
    GROUP BY owner_id
) AS a USING owner_id
%s
ORDER BY %s
LIMIT ? OFFSET ?`, searchWhere, orderBy)
	args := []any{from.UTC(), to.UTC(), from.UTC(), to.UTC()}
	args = append(args, searchArgs...)
	args = append(args, limit, offset)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query user usage report: %w", err)
	}
	defer rows.Close()

	items := make([]model.UserUsageReportItem, 0)
	for rows.Next() {
		var item model.UserUsageReportItem
		if err := rows.Scan(&item.OwnerID, &item.OwnerUsername, &item.MemoryUsageBytes, &item.ReservedMemoryBytes, &item.CPUPercent, &item.TotalDiskBytes, &item.ResourcesTotal, &item.ContainersTotal, &item.ContainersRunning, &item.VolumesTotal, &item.ImagesTotal, &item.BuildsTotal, &item.ProjectsTotal, &item.ActionsTotal); err != nil {
			return nil, 0, fmt.Errorf("failed to scan user usage report: %w", err)
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func userUsageSearchWhere(search string) (string, []any) {
	search = strings.TrimSpace(search)
	if search == "" {
		return "", nil
	}
	return "WHERE (positionCaseInsensitive(owner_username, ?) > 0 OR positionCaseInsensitive(toString(owner_id), ?) > 0)", []any{search, search}
}

func (r *ReportsRepository) GetUserUsageTimeline(ctx context.Context, ownerID uuid.UUID, from, to time.Time, bucketInterval time.Duration) ([]model.UserUsagePoint, error) {
	db, err := r.dbConn()
	if err != nil {
		return nil, err
	}
	intervalSeconds := int64(bucketInterval / time.Second)
	if intervalSeconds <= 0 {
		intervalSeconds = 300
	}
	rows, err := db.QueryContext(ctx, `
SELECT
    u.bucket_start,
    u.memory_usage_bytes,
    u.reserved_memory_bytes,
    u.cpu_percent,
    u.total_disk_bytes,
    u.containers_total + u.volumes_total + u.images_total + u.builds_total + u.projects_total AS resources_total,
    u.containers_total,
    u.containers_running,
    u.volumes_total,
    u.images_total,
    u.builds_total,
    u.projects_total,
    ifNull(a.actions_total, 0) AS actions_total
FROM (
    SELECT
        bucket_start,
        argMax(memory_usage_bytes, collected_at) AS memory_usage_bytes,
        argMax(reserved_memory_bytes, collected_at) AS reserved_memory_bytes,
        argMax(cpu_percent, collected_at) AS cpu_percent,
        argMax(total_disk_bytes, collected_at) AS total_disk_bytes,
        argMax(containers_total, collected_at) AS containers_total,
        argMax(containers_running, collected_at) AS containers_running,
        argMax(volumes_total, collected_at) AS volumes_total,
        argMax(images_total, collected_at) AS images_total,
        argMax(builds_total, collected_at) AS builds_total,
        argMax(projects_total, collected_at) AS projects_total
    FROM user_resource_usage_snapshots
    WHERE owner_id = ? AND bucket_start >= ? AND bucket_start < ?
    GROUP BY bucket_start
) AS u
LEFT JOIN (
    SELECT toDateTime(intDiv(toUnixTimestamp(occurred_at), ?) * ?, 'UTC') AS bucket_start, count() AS actions_total
    FROM audit_events
    WHERE actor_user_id = ? AND occurred_at >= ? AND occurred_at < ?
    GROUP BY bucket_start
) AS a USING bucket_start
ORDER BY bucket_start ASC`, ownerID, from.UTC(), to.UTC(), intervalSeconds, intervalSeconds, ownerID, from.UTC(), to.UTC())
	if err != nil {
		return nil, fmt.Errorf("failed to query user usage timeline: %w", err)
	}
	defer rows.Close()

	points := make([]model.UserUsagePoint, 0)
	for rows.Next() {
		var point model.UserUsagePoint
		if err := rows.Scan(&point.BucketStart, &point.MemoryUsageBytes, &point.ReservedMemoryBytes, &point.CPUPercent, &point.TotalDiskBytes, &point.ResourcesTotal, &point.ContainersTotal, &point.ContainersRunning, &point.VolumesTotal, &point.ImagesTotal, &point.BuildsTotal, &point.ProjectsTotal, &point.ActionsTotal); err != nil {
			return nil, fmt.Errorf("failed to scan user usage timeline: %w", err)
		}
		points = append(points, point)
	}
	return points, rows.Err()
}

func (r *ReportsRepository) ListAuditEvents(ctx context.Context, filters model.AuditEventFilters) ([]model.AuditEvent, int, error) {
	db, err := r.dbConn()
	if err != nil {
		return nil, 0, err
	}
	where, args := auditWhere(filters)

	var total int
	countQuery := "SELECT count() FROM audit_events " + where
	if err := db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count audit events: %w", err)
	}

	query := `
SELECT
    id, occurred_at, actor_user_id, actor_username, actor_scope, action, outcome,
    resource_type, resource_id, resource_name, owner_id, owner_username,
    request_id, client_ip, user_agent, error_code, details_json
FROM audit_events ` + where + `
ORDER BY occurred_at DESC
LIMIT ? OFFSET ?`
	args = append(args, filters.Limit, filters.Offset)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query audit events: %w", err)
	}
	defer rows.Close()

	events := make([]model.AuditEvent, 0)
	for rows.Next() {
		var event model.AuditEvent
		var actorID, ownerID uuid.UUID
		if err := rows.Scan(
			&event.ID,
			&event.OccurredAt,
			&actorID,
			&event.ActorUsername,
			&event.ActorScope,
			&event.Action,
			&event.Outcome,
			&event.ResourceType,
			&event.ResourceID,
			&event.ResourceName,
			&ownerID,
			&event.OwnerUsername,
			&event.RequestID,
			&event.ClientIP,
			&event.UserAgent,
			&event.ErrorCode,
			&event.DetailsJSON,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan audit event: %w", err)
		}
		event.ActorUserID = parseUUIDZeroAsNil(actorID)
		event.OwnerID = parseUUIDZeroAsNil(ownerID)
		events = append(events, event)
	}
	return events, total, rows.Err()
}

func auditWhere(filters model.AuditEventFilters) (string, []any) {
	parts := []string{"occurred_at >= ?", "occurred_at < ?"}
	args := []any{filters.From.UTC(), filters.To.UTC()}
	if filters.ActorUserID != nil {
		parts = append(parts, "actor_user_id = ?")
		args = append(args, *filters.ActorUserID)
	}
	if filters.Action != "" {
		parts = append(parts, "action = ?")
		args = append(args, filters.Action)
	}
	if filters.Outcome != "" {
		parts = append(parts, "outcome = ?")
		args = append(args, filters.Outcome)
	}
	if filters.ResourceType != "" {
		parts = append(parts, "resource_type = ?")
		args = append(args, filters.ResourceType)
	}
	if filters.Search != "" {
		parts = append(parts, "(positionCaseInsensitive(actor_username, ?) > 0 OR positionCaseInsensitive(resource_name, ?) > 0 OR positionCaseInsensitive(resource_id, ?) > 0 OR positionCaseInsensitive(details_json, ?) > 0)")
		args = append(args, filters.Search, filters.Search, filters.Search, filters.Search)
	}
	return "WHERE " + strings.Join(parts, " AND "), args
}

func nullableUUID(value *uuid.UUID) any {
	if value == nil || *value == uuid.Nil {
		return uuid.Nil
	}
	return *value
}

func parseUUIDZeroAsNil(value uuid.UUID) *uuid.UUID {
	if value == uuid.Nil {
		return nil
	}
	return &value
}
