package dbtest_test

import (
	"context"
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

// registryAccessMigration is the Core migration that creates the Registry
// tables.
const registryAccessMigration = 9

// The Registry migration creates the one Registry GC state row, free and not
// draining; the table holds no second row, and down removes it.
func TestCoreMigrationCreatesTheRegistryGCStateRow(t *testing.T) {
	m, db := dbtest.NewCoreMigration(t)
	ctx := context.Background()
	requireSingleton := func() {
		t.Helper()
		var rows int
		var leaseFree, notDraining, completedSet bool
		require.NoError(t, db.QueryRow(ctx, `SELECT count(*) OVER (), lease_until IS NULL, draining_since IS NULL, last_completed_at IS NOT NULL FROM registry_gc_state`).Scan(&rows, &leaseFree, &notDraining, &completedSet))
		require.Equal(t, 1, rows)
		require.True(t, leaseFree)
		require.True(t, notDraining)
		require.True(t, completedSet)
	}
	require.NoError(t, m.Migrate(registryAccessMigration))
	requireSingleton()
	_, err := db.Exec(ctx, `INSERT INTO registry_gc_state (id) VALUES (FALSE)`)
	require.Error(t, err, "the state row is a singleton")

	require.NoError(t, m.Migrate(registryAccessMigration-1))
	var exists bool
	require.NoError(t, db.QueryRow(ctx, `SELECT to_regclass('registry_gc_state') IS NOT NULL`).Scan(&exists))
	require.False(t, exists)

	require.NoError(t, m.Migrate(registryAccessMigration))
	requireSingleton()
}
