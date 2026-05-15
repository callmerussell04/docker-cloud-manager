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

	"github.com/google/uuid"
	_ "github.com/lib/pq"
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
