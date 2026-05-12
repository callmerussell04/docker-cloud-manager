package clickhouse_test

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	clickhouserepo "github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/clickhouse"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/auditlog"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestReportsRepositoryInsertUsageSnapshotsLeavesConnectionReusable(t *testing.T) {
	cfg, ok := clickHouseTestConfig()
	if !ok {
		t.Skip("set DCM_CLICKHOUSE_TEST_ADDR to run ClickHouse reports integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openClickHouseTestDB(t, cfg)
	defer db.Close()
	applyReportsMigrations(t, ctx, db)

	repo := clickhouserepo.NewReportsRepository(cfg)
	defer repo.Close()

	ownerID := uuid.New()
	actorID := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)
	bucketStart := now.Truncate(5 * time.Minute)

	require.NoError(t, repo.InsertUsageSnapshots(ctx, []model.UsageSnapshot{{
		OwnerID:             ownerID,
		OwnerUsername:       "alice",
		BucketStart:         bucketStart,
		CollectedAt:         now,
		ReservedMemoryBytes: 512,
		ImageDiskBytes:      1024,
		VolumeDiskBytes:     2048,
		TotalDiskBytes:      3072,
		ContainersTotal:     2,
		ContainersRunning:   1,
		VolumesTotal:        1,
		ImagesTotal:         1,
		BuildsTotal:         0,
		ProjectsTotal:       1,
	}}))

	require.NoError(t, repo.RecordAuditEvent(ctx, model.AuditEvent{
		ID:            uuid.New(),
		OccurredAt:    now,
		ActorUserID:   &actorID,
		ActorUsername: "alice",
		ActorScope:    auditlog.ActorScopeUser,
		Action:        auditlog.ActionContainerExpose,
		Outcome:       auditlog.OutcomeSuccess,
		ResourceType:  auditlog.ResourceContainer,
		ResourceID:    "container-1",
		ResourceName:  "web",
		OwnerID:       &ownerID,
		OwnerUsername: "alice",
		DetailsJSON:   `{"domain_prefix":"demo"}`,
	}))

	from := bucketStart.Add(-time.Minute)
	to := now.Add(time.Minute)

	events, total, err := repo.ListAuditEvents(ctx, model.AuditEventFilters{
		From:   from,
		To:     to,
		Search: "demo",
		Limit:  20,
	})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, events, 1)
	require.Equal(t, auditlog.ActionContainerExpose, events[0].Action)

	overview, err := repo.GetReportsOverview(ctx, from, to)
	require.NoError(t, err)
	require.EqualValues(t, 1, overview.AuditEventsTotal)
	require.EqualValues(t, 512, overview.ReservedMemoryBytes)
	require.EqualValues(t, 3072, overview.TotalDiskBytes)

	latest, err := repo.GetLatestUsageSnapshotCollectedAt(ctx)
	require.NoError(t, err)
	require.NotNil(t, latest)
	require.False(t, latest.IsZero())
}

func clickHouseTestConfig() (clickhouserepo.Config, bool) {
	addr := strings.TrimSpace(os.Getenv("DCM_CLICKHOUSE_TEST_ADDR"))
	if addr == "" {
		return clickhouserepo.Config{}, false
	}
	return clickhouserepo.Config{
		Addr:     addr,
		Database: envOrDefault("DCM_CLICKHOUSE_TEST_DB", "dcm_reports_test"),
		Username: envOrDefault("DCM_CLICKHOUSE_TEST_USER", "default"),
		Password: os.Getenv("DCM_CLICKHOUSE_TEST_PASSWORD"),
	}, true
}

func openClickHouseTestDB(t *testing.T, cfg clickhouserepo.Config) *sql.DB {
	t.Helper()
	db, err := sql.Open("clickhouse", clickHouseDSN(cfg))
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	return db
}

func applyReportsMigrations(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	migrationsDir := filepath.Join("..", "..", "..", "..", "migrations", "reports")
	downFiles, err := filepath.Glob(filepath.Join(migrationsDir, "*.down.sql"))
	require.NoError(t, err)
	sort.Sort(sort.Reverse(sort.StringSlice(downFiles)))
	for _, file := range downFiles {
		execMigrationFile(t, ctx, db, file)
	}

	upFiles, err := filepath.Glob(filepath.Join(migrationsDir, "*.up.sql"))
	require.NoError(t, err)
	sort.Strings(upFiles)
	for _, file := range upFiles {
		execMigrationFile(t, ctx, db, file)
	}
}

func execMigrationFile(t *testing.T, ctx context.Context, db *sql.DB, path string) {
	t.Helper()
	query, err := os.ReadFile(path)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(query))
	require.NoErrorf(t, err, "migration %s failed", path)
}

func clickHouseDSN(cfg clickhouserepo.Config) string {
	u := url.URL{
		Scheme: "clickhouse",
		Host:   cfg.Addr,
		Path:   "/" + strings.TrimLeft(cfg.Database, "/"),
	}
	q := u.Query()
	if cfg.Username != "" {
		q.Set("username", cfg.Username)
	}
	if cfg.Password != "" {
		q.Set("password", cfg.Password)
	}
	q.Set("dial_timeout", "5s")
	q.Set("compress", "lz4")
	u.RawQuery = q.Encode()
	return u.String()
}

func envOrDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
