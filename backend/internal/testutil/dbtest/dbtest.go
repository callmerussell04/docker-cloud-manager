// Package dbtest opens isolated PostgreSQL schemas for repository and
// integration tests.
package dbtest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/pgpool"
)

// OpenCorePostgres opens a pool with the default budget in a fresh schema
// holding the Core migrations.
func OpenCorePostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("CORE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CORE_TEST_DATABASE_URL is not set")
	}
	requireSafeDatabase(t, dsn, "CORE_TEST_DATABASE_URL", "CORE_TEST_DATABASE_ALLOW_UNSAFE")

	db := openPostgresSchema(t, dsn, "core_test_")
	applyMigrations(t, db, "core")
	return db
}

// OpenSSOPostgres opens a pool with the default budget in a fresh schema
// holding the SSO migrations.
func OpenSSOPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("SSO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SSO_TEST_DATABASE_URL is not set")
	}
	requireSafeDatabase(t, dsn, "SSO_TEST_DATABASE_URL", "SSO_TEST_DATABASE_ALLOW_UNSAFE")

	db := openPostgresSchema(t, dsn, "sso_test_")
	applyMigrations(t, db, "sso")
	return db
}

func openPostgresSchema(t *testing.T, dsn, prefix string) *pgxpool.Pool {
	t.Helper()
	schema := prefix + strings.ReplaceAll(uuid.NewString(), "-", "")

	adminDB := openPool(t, dsn, pgpool.DefaultConfig())
	ctx := context.Background()
	if _, err := adminDB.Exec(ctx, `CREATE SCHEMA `+quoteIdent(schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+quoteIdent(schema)+` CASCADE`)
	})

	return openPool(t, withSearchPath(t, dsn, schema), pgpool.DefaultConfig())
}

// pending counts acquisitions started but not finished, per pool, because
// pgxpool statistics count an acquire only once it completes.
var pending sync.Map // *pgxpool.Pool -> *atomic.Int64

type acquireTracer struct{}

func (acquireTracer) TraceAcquireStart(ctx context.Context, pool *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	counter(pool).Add(1)
	return ctx
}

func (acquireTracer) TraceAcquireEnd(_ context.Context, pool *pgxpool.Pool, _ pgxpool.TraceAcquireEndData) {
	counter(pool).Add(-1)
}

// pgx accepts a tracer only through ConnConfig.Tracer, which must be a QueryTracer.
func (acquireTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}

func (acquireTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func counter(pool *pgxpool.Pool) *atomic.Int64 {
	value, _ := pending.LoadOrStore(pool, new(atomic.Int64))
	return value.(*atomic.Int64)
}

// PendingAcquires reports how many callers are acquiring, or waiting to
// acquire, a connection from pool. Only pools opened by dbtest are traced.
func PendingAcquires(pool *pgxpool.Pool) int64 {
	return counter(pool).Load()
}

func openPool(t *testing.T, dsn string, budget pgpool.Config) *pgxpool.Pool {
	t.Helper()
	poolCfg, err := pgpool.NewPoolConfig(dsn, budget)
	if err != nil {
		t.Fatalf("configure test pool: %v", err)
	}
	poolCfg.ConnConfig.Tracer = acquireTracer{}
	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		pending.Delete(pool)
	})
	return pool
}

func applyMigrations(t *testing.T, db *pgxpool.Pool, name string) {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve dbtest path")
	}
	backendRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", ".."))
	files, err := filepath.Glob(filepath.Join(backendRoot, "migrations", name, "*.up.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no %s migrations found", name)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		for _, stmt := range strings.Split(string(data), ";") {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if _, err := db.Exec(context.Background(), stmt); err != nil {
				t.Fatalf("apply migration %s: %v\n%s", file, err, stmt)
			}
		}
	}
}

func requireSafeDatabase(t *testing.T, dsn, envName, allowEnvName string) {
	t.Helper()
	if os.Getenv(allowEnvName) == "1" {
		return
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse %s: %v", envName, err)
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if !strings.Contains(strings.ToLower(dbName), "test") {
		t.Fatalf("refusing to run repository tests against database %q; use a test database or set %s=1", dbName, allowEnvName)
	}
}

func withSearchPath(t *testing.T, dsn string, schema string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func CountRows(t *testing.T, db *pgxpool.Pool, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(context.Background(), fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteIdent(table))).Scan(&count); err != nil {
		t.Fatalf("count rows in %s: %v", table, err)
	}
	return count
}

// OpenCorePeerPool opens a distinct pool with budget in db's isolated Core schema.
func OpenCorePeerPool(t *testing.T, db *pgxpool.Pool, budget pgpool.Config) *pgxpool.Pool {
	t.Helper()
	return openPeerPool(t, db, "CORE_TEST_DATABASE_URL", budget)
}

// OpenSSOPeerPool opens a distinct pool with budget in db's isolated SSO schema.
func OpenSSOPeerPool(t *testing.T, db *pgxpool.Pool, budget pgpool.Config) *pgxpool.Pool {
	t.Helper()
	return openPeerPool(t, db, "SSO_TEST_DATABASE_URL", budget)
}

func openPeerPool(t *testing.T, db *pgxpool.Pool, env string, budget pgpool.Config) *pgxpool.Pool {
	t.Helper()
	var schema string
	if err := db.QueryRow(context.Background(), `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	return openPool(t, withSearchPath(t, os.Getenv(env), schema), budget)
}

// Budget returns the default budget capped at maxConns connections.
func Budget(maxConns int) pgpool.Config {
	cfg := pgpool.DefaultConfig()
	cfg.MaxConns = maxConns
	return cfg
}

// CheckPoolProgress exercises blocked SQL, canceled SQL and queued acquisition.
// The peer is an external lock holder, outside the application's pool budget.
func CheckPoolProgress(t *testing.T, db, peer *pgxpool.Pool, table string, query func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := peer.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }() // no-op once the explicit rollback below ran
	_, err = tx.Exec(ctx, `LOCK TABLE `+quoteIdent(table)+` IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	blockedCtx, cancelBlocked := context.WithCancel(ctx)
	defer cancelBlocked()
	canceled := make(chan error, 1)
	go func() { canceled <- query(blockedCtx) }()
	require.Eventually(t, func() bool { return db.Stat().AcquiredConns() == db.Stat().MaxConns() }, time.Second, time.Millisecond)
	results := make(chan error, 12)
	for range 12 {
		go func() { results <- query(ctx) }()
	}
	require.Eventually(t, func() bool { return PendingAcquires(db) >= 12 }, time.Second, time.Millisecond)
	require.LessOrEqual(t, db.Stat().TotalConns(), db.Stat().MaxConns())
	cancelBlocked()
	select {
	case err := <-canceled:
		require.Error(t, err)
	case <-ctx.Done():
		t.Fatal("blocked query did not cancel")
	}
	// A queued acquisition must preserve context cancellation too.
	queuedCtx, cancelQueued := context.WithCancel(ctx)
	cancelQueued()
	require.ErrorIs(t, query(queuedCtx), context.Canceled)
	require.NoError(t, tx.Rollback(ctx))
	for range 12 {
		select {
		case err := <-results:
			require.NoError(t, err)
		case <-ctx.Done():
			t.Fatal("queries did not progress")
		}
	}
	require.LessOrEqual(t, db.Stat().TotalConns(), db.Stat().MaxConns())
}
