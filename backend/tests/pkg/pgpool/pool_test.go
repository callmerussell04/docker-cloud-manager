package pgpool_test

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/pkg/pgpool"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestPoolBudgets(t *testing.T) {
	for _, tc := range []struct {
		name               string
		open, idle         int
		lifetime, idleTime time.Duration
		valid              bool
	}{
		{"minimum", 1, 0, time.Minute, time.Minute, true},
		{"default", 20, 5, 30 * time.Minute, 5 * time.Minute, true},
		{"unlimited", 0, 0, time.Minute, time.Minute, false},
		{"negative", -1, 0, time.Minute, time.Minute, false},
		{"idle exceeds open", 2, 3, time.Minute, time.Minute, false},
		{"negative idle", 2, -1, time.Minute, time.Minute, false},
		{"unlimited lifetime", 2, 1, 0, time.Minute, false},
		{"negative idle time", 2, 1, time.Minute, -time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := pgpool.Config{MaxOpen: tc.open, MaxIdle: tc.idle, MaxLifetime: tc.lifetime, MaxIdleTime: tc.idleTime}
			db, err := sql.Open("postgres", "")
			require.NoError(t, err)
			defer func() { _ = db.Close() }() // test teardown; no connection was opened
			err = cfg.Apply(db)
			if !tc.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.open, db.Stats().MaxOpenConnections)
			query, admission, err := cfg.CoreBudgets()
			if tc.open < 2 {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.open, query.MaxOpen+admission.MaxOpen)
			require.Equal(t, tc.idle, query.MaxIdle+admission.MaxIdle)
			require.Equal(t, 1, admission.MaxOpen)
		})
	}
}

func TestParsePoolBootstrapSettings(t *testing.T) {
	for _, prefix := range []string{"CORE", "SSO"} {
		t.Run(prefix, func(t *testing.T) {
			values := map[string]string{prefix + "_DB_MAX_OPEN": "2", prefix + "_DB_MAX_IDLE": "0", prefix + "_DB_MAX_LIFETIME": "20m", prefix + "_DB_MAX_IDLE_TIME": "1m"}
			cfg, err := pgpool.Parse(func(key string) (string, bool) { value, ok := values[key]; return value, ok }, prefix)
			require.NoError(t, err)
			require.Equal(t, pgpool.Config{MaxOpen: 2, MaxIdle: 0, MaxLifetime: 20 * time.Minute, MaxIdleTime: time.Minute}, cfg)
			for _, suffix := range []string{"MAX_OPEN", "MAX_IDLE", "MAX_LIFETIME", "MAX_IDLE_TIME"} {
				key := prefix + "_DB_" + suffix
				previous := values[key]
				values[key] = "secret-invalid-value"
				_, err := pgpool.Parse(func(key string) (string, bool) { value, ok := values[key]; return value, ok }, prefix)
				require.ErrorContains(t, err, key)
				require.NotContains(t, err.Error(), "secret-invalid-value")
				values[key] = previous
			}
		})
	}
	cfg, err := pgpool.Parse(func(string) (string, bool) { return "", false }, "CORE")
	require.NoError(t, err)
	require.Equal(t, pgpool.DefaultConfig(), cfg)
}

func TestPoolDiagnosticsCancelWithoutDatabaseAccess(t *testing.T) {
	db, err := sql.Open("postgres", "")
	require.NoError(t, err)
	defer func() { _ = db.Close() }() // test teardown; no connection was opened
	require.NoError(t, pgpool.DefaultConfig().Apply(db))
	var output strings.Builder
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pgpool.LogStats(ctx, logger, map[string]*sql.DB{"queries": db})
	require.Contains(t, output.String(), `"max_open":20`)
	require.Contains(t, output.String(), `"wait_count":0`)
	require.Zero(t, db.Stats().OpenConnections)
}
