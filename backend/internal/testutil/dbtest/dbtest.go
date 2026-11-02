package dbtest

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func OpenCorePostgres(t *testing.T) *sql.DB {
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

func OpenSSOPostgres(t *testing.T) *sql.DB {
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

func openPostgresSchema(t *testing.T, dsn, prefix string) *sql.DB {
	t.Helper()
	schema := prefix + strings.ReplaceAll(uuid.NewString(), "-", "")

	adminDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open admin db: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })

	ctx := context.Background()
	if _, err := adminDB.ExecContext(ctx, `CREATE SCHEMA `+quoteIdent(schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+quoteIdent(schema)+` CASCADE`)
	})

	db, err := sql.Open("postgres", withSearchPath(t, dsn, schema))
	if err != nil {
		t.Fatalf("open schema db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return db
}

func applyMigrations(t *testing.T, db *sql.DB, name string) {
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
			if _, err := db.Exec(stmt); err != nil {
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

func CountRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteIdent(table))).Scan(&count); err != nil {
		t.Fatalf("count rows in %s: %v", table, err)
	}
	return count
}

// OpenCoreAdmissionPool opens a distinct pool in the existing isolated schema.
func OpenCoreAdmissionPool(t *testing.T, db *sql.DB) *sql.DB {
	t.Helper()
	return openPeerPool(t, db, "CORE_TEST_DATABASE_URL")
}

func OpenSSOPeerPool(t *testing.T, db *sql.DB) *sql.DB {
	t.Helper()
	return openPeerPool(t, db, "SSO_TEST_DATABASE_URL")
}

func openPeerPool(t *testing.T, db *sql.DB, env string) *sql.DB {
	t.Helper()
	var schema string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	pool, err := sql.Open("postgres", withSearchPath(t, os.Getenv(env), schema))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return pool
}

// CheckPoolProgress exercises blocked SQL, canceled SQL and queued acquisition.
// The peer is an external lock holder, outside the application's pool budget.
func CheckPoolProgress(t *testing.T, db, peer *sql.DB, table string, query func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := peer.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `LOCK TABLE `+quoteIdent(table)+` IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	blockedCtx, cancelBlocked := context.WithCancel(ctx)
	defer cancelBlocked()
	canceled := make(chan error, 1)
	go func() { canceled <- query(blockedCtx) }()
	require.Eventually(t, func() bool { return db.Stats().InUse == 1 }, time.Second, time.Millisecond)
	results := make(chan error, 12)
	for range 12 {
		go func() { results <- query(ctx) }()
	}
	require.Eventually(t, func() bool { return db.Stats().WaitCount >= 12 }, time.Second, time.Millisecond)
	require.LessOrEqual(t, db.Stats().OpenConnections, db.Stats().MaxOpenConnections)
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
	require.NoError(t, tx.Rollback())
	for range 12 {
		select {
		case err := <-results:
			require.NoError(t, err)
		case <-ctx.Done():
			t.Fatal("queries did not progress")
		}
	}
	require.LessOrEqual(t, db.Stats().OpenConnections, db.Stats().MaxOpenConnections)
}
