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

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the pgx5 driver production migrations use
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/pgpool"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/testenv"
)

// OpenCorePostgres opens a pool with the default budget in a fresh schema
// holding the Core migrations.
func OpenCorePostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return openMigratedSchema(t, coreDSNKey, coreAllowUnsafeKey, "core")
}

// OpenSSOPostgres opens a pool with the default budget in a fresh schema
// holding the SSO migrations.
func OpenSSOPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return openMigratedSchema(t, ssoDSNKey, ssoAllowUnsafeKey, "sso")
}

const (
	coreDSNKey         = "CORE_TEST_DATABASE_URL"
	coreAllowUnsafeKey = "CORE_TEST_DATABASE_ALLOW_UNSAFE"
	ssoDSNKey          = "SSO_TEST_DATABASE_URL"
	ssoAllowUnsafeKey  = "SSO_TEST_DATABASE_ALLOW_UNSAFE"

	// sharedSchema holds the database-wide extensions every test schema uses.
	sharedSchema = "public"
	// extensionLockID serializes extension setup across test processes.
	extensionLockID = 0x64636d74657374 // "dcmtest"
)

// NewCoreMigration returns the production migration tool over a fresh, empty
// database for the Core migrations, and a pool in that database. A database of
// its own lets down migrations drop extensions without touching other tests.
func NewCoreMigration(t *testing.T) (*migrate.Migrate, *pgxpool.Pool) {
	t.Helper()
	dsn := openDatabase(t, safeDSN(t, coreDSNKey, coreAllowUnsafeKey), "core_migration_test_")
	return migrator(t, dsn, "core"), openPool(t, dsn, pgpool.DefaultConfig())
}

// NewSSOMigration returns the production migration tool over a fresh, empty
// database for the SSO migrations, and a pool in that database.
func NewSSOMigration(t *testing.T) (*migrate.Migrate, *pgxpool.Pool) {
	t.Helper()
	dsn := openDatabase(t, safeDSN(t, ssoDSNKey, ssoAllowUnsafeKey), "sso_migration_test_")
	return migrator(t, dsn, "sso"), openPool(t, dsn, pgpool.DefaultConfig())
}

func openMigratedSchema(t *testing.T, dsnKey, allowUnsafeKey, name string) *pgxpool.Pool {
	t.Helper()
	dsn := openSchema(t, safeDSN(t, dsnKey, allowUnsafeKey), name+"_test_")
	if err := migrator(t, dsn, name).Up(); err != nil {
		t.Fatalf("apply %s migrations: %v", name, err)
	}
	return openPool(t, dsn, pgpool.DefaultConfig())
}

// safeDSN returns the test database named by dsnKey, or ends t through its
// integration gate. allowUnsafeKey permits a database not named as a test one.
func safeDSN(t *testing.T, dsnKey, allowUnsafeKey string) string {
	t.Helper()
	dsn := testenv.Lookup(t, dsnKey)
	requireSafeDatabase(t, dsn, dsnKey, allowUnsafeKey)
	return dsn
}

// migrator returns golang-migrate, the production migration tool, reading the
// named migrations through its pgx v5 driver into the current schema of dsn.
func migrator(t *testing.T, dsn, name string) *migrate.Migrate {
	t.Helper()
	source, err := iofs.New(os.DirFS(migrationsDir(t, name)), ".")
	if err != nil {
		t.Fatalf("open %s migrations: %v", name, err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.Scheme = "pgx5"
	m, err := migrate.NewWithSourceInstance("iofs", source, u.String())
	if err != nil {
		t.Fatalf("open %s migrator: %v", name, err)
	}
	t.Cleanup(func() { _, _ = m.Close() }) // test teardown; the schema or database is dropped next
	return m
}

// openSchema creates a schema unique to t, drops it when t ends, and returns
// dsn searching that schema and then the shared one. Extensions live in the
// shared schema, installed once, so parallel tests never race to create them
// and dropping a test schema never drops them.
func openSchema(t *testing.T, dsn, prefix string) string {
	t.Helper()
	schema := prefix + strings.ReplaceAll(uuid.NewString(), "-", "")

	adminDB := openPool(t, dsn, pgpool.DefaultConfig())
	ctx := context.Background()
	ensureSharedExtensions(t, adminDB)
	if _, err := adminDB.Exec(ctx, `CREATE SCHEMA `+quoteIdent(schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+quoteIdent(schema)+` CASCADE`)
	})

	return withSearchPath(t, dsn, schema)
}

func ensureSharedExtensions(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	err := pgx.BeginFunc(context.Background(), db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(context.Background(), `SELECT pg_advisory_xact_lock($1)`, int64(extensionLockID)); err != nil {
			return err
		}
		_, err := tx.Exec(context.Background(), `CREATE EXTENSION IF NOT EXISTS pgcrypto SCHEMA `+quoteIdent(sharedSchema))
		return err
	})
	if err != nil {
		t.Fatalf("install shared extensions: %v", err)
	}
}

// openDatabase creates a database unique to t on the server of dsn, drops it
// when t ends, and returns dsn pointing at it.
func openDatabase(t *testing.T, dsn, prefix string) string {
	t.Helper()
	name := prefix + strings.ReplaceAll(uuid.NewString(), "-", "")
	adminDB := openPool(t, dsn, pgpool.DefaultConfig())
	if _, err := adminDB.Exec(context.Background(), `CREATE DATABASE `+quoteIdent(name)); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		// Best effort: the database is unique to t, and a failed drop only leaves a disposable test database.
		_, _ = adminDB.Exec(context.Background(), `DROP DATABASE IF EXISTS `+quoteIdent(name)+` WITH (FORCE)`)
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.Path = "/" + name
	return u.String()
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

func migrationsDir(t *testing.T, name string) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve dbtest path")
	}
	backendRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", ".."))
	return filepath.Join(backendRoot, "migrations", name)
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
	q.Set("search_path", schema+","+sharedSchema)
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
	return openPeerPool(t, db, coreDSNKey, budget)
}

// OpenSSOPeerPool opens a distinct pool with budget in db's isolated SSO schema.
func OpenSSOPeerPool(t *testing.T, db *pgxpool.Pool, budget pgpool.Config) *pgxpool.Pool {
	t.Helper()
	return openPeerPool(t, db, ssoDSNKey, budget)
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
