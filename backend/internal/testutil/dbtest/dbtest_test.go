package dbtest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
)

func TestMigrationsUpDownUp(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) (*migrate.Migrate, *pgxpool.Pool)
	}{
		{name: "core", open: dbtest.NewCoreMigration},
		{name: "sso", open: dbtest.NewSSOMigration},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, db := tt.open(t)

			require.NoError(t, m.Up())
			migrated := schemaShape(t, db)
			require.NotEmpty(t, migrated)

			require.NoError(t, m.Down())
			require.Empty(t, schemaShape(t, db), "down migrations leave no application objects behind")

			require.NoError(t, m.Up())
			require.Equal(t, migrated, schemaShape(t, db), "re-applied migrations rebuild the same schema")
		})
	}
}

// schemaShape lists the columns, indexes, constraints, functions and types of
// the current schema, excluding golang-migrate's own version table.
func schemaShape(t *testing.T, db *pgxpool.Pool) []string {
	t.Helper()
	rows, err := db.Query(context.Background(), `
		SELECT 'column ' || table_name || '.' || column_name || ' ' || data_type || ' ' || is_nullable || ' ' || COALESCE(column_default, '')
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name <> 'schema_migrations'
		UNION ALL
		SELECT 'index ' || indexname || ' ' || regexp_replace(indexdef, ' ON \S+\.', ' ON ')
		FROM pg_indexes
		WHERE schemaname = current_schema() AND tablename <> 'schema_migrations'
		UNION ALL
		SELECT 'constraint ' || c.conname || ' ' || pg_get_constraintdef(c.oid)
		FROM pg_constraint c JOIN pg_namespace n ON n.oid = c.connamespace
		WHERE n.nspname = current_schema() AND c.conrelid <> COALESCE(to_regclass('schema_migrations')::oid, 0)
		UNION ALL
		SELECT 'function ' || p.proname
		FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = current_schema()
		UNION ALL
		SELECT 'type ' || t.typname
		FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE n.nspname = current_schema() AND t.typtype IN ('e', 'd')
		ORDER BY 1
	`)
	require.NoError(t, err)
	shape, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	return shape
}

// Migration 0019 gives every image row the Registry tag it is served under:
// an unclaimed build its own image ID, every other row the version it was
// pushed under. It deletes building rows whose build no longer runs.
func TestCoreMigration0019BackfillsRegistryTags(t *testing.T) {
	m, db := dbtest.NewCoreMigration(t)
	ctx := context.Background()
	require.NoError(t, m.Migrate(18))

	type row struct {
		tag, imageStatus, buildStatus string
	}
	rows := map[string]row{
		"available":            {tag: "app:v1", imageStatus: "available", buildStatus: "success"},
		"available no build":   {tag: "app:v2", imageStatus: "available"},
		"missing":              {tag: "app:v3", imageStatus: "missing"},
		"pending build":        {tag: "app:v4", imageStatus: "building", buildStatus: "pending"},
		"running build":        {tag: "app:v5", imageStatus: "building", buildStatus: "running"},
		"orphan without build": {tag: "app:v6", imageStatus: "building"},
		"orphan canceled":      {tag: "app:v7", imageStatus: "building", buildStatus: "canceled"},
		"no version":           {tag: "bare", imageStatus: "available"},
	}
	ids := map[string]string{}
	owner := "00000000-0000-0000-0000-000000000001"
	for name, r := range rows {
		var id string
		require.NoError(t, db.QueryRow(ctx, `INSERT INTO images (owner_id, tag, size_mb, status) VALUES ($1, $2, 1, $3) RETURNING id::text`, owner, r.tag, r.imageStatus).Scan(&id))
		ids[name] = id
		if r.buildStatus != "" {
			_, err := db.Exec(ctx, `INSERT INTO builds (image_id, owner_id, status, log_file_path) VALUES ($1, $2, $3, 'log')`, id, owner, r.buildStatus)
			require.NoError(t, err)
		}
	}

	require.NoError(t, m.Migrate(19))

	registryTag := func(name string) (string, bool) {
		var tag string
		err := db.QueryRow(ctx, `SELECT registry_tag FROM images WHERE id = $1`, ids[name]).Scan(&tag)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false
		}
		require.NoError(t, err)
		return tag, true
	}
	want := map[string]string{
		"available":          "v1",
		"available no build": "v2",
		"missing":            "v3",
		"pending build":      ids["pending build"],
		"running build":      "v5",
		"no version":         "latest",
	}
	for name, tag := range want {
		got, ok := registryTag(name)
		require.True(t, ok, name)
		require.Equal(t, tag, got, name)
	}
	for _, name := range []string{"orphan without build", "orphan canceled"} {
		_, ok := registryTag(name)
		require.False(t, ok, "%s is deleted", name)
	}
	var digests int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM images WHERE digest IS NOT NULL`).Scan(&digests))
	require.Zero(t, digests)

	require.NoError(t, m.Migrate(18))
	require.NoError(t, m.Migrate(19))
}
