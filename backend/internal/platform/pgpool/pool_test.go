package pgpool_test

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/pgpool"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestPoolBudgets(t *testing.T) {
	for _, tc := range []struct {
		name               string
		maxConns           int
		lifetime, idleTime time.Duration
		valid              bool
	}{
		{"minimum", 1, time.Minute, time.Minute, true},
		{"default", 20, 30 * time.Minute, 5 * time.Minute, true},
		{"split", 4, time.Minute, time.Minute, true},
		{"unlimited", 0, time.Minute, time.Minute, false},
		{"negative", -1, time.Minute, time.Minute, false},
		{"unlimited lifetime", 2, 0, time.Minute, false},
		{"negative idle time", 2, time.Minute, -time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := pgpool.Config{MaxConns: tc.maxConns, MaxConnLifetime: tc.lifetime, MaxConnIdleTime: tc.idleTime}
			poolCfg, err := pgxpool.ParseConfig("")
			require.NoError(t, err)
			err = cfg.Apply(poolCfg)
			if !tc.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, tc.maxConns, poolCfg.MaxConns)
			require.Equal(t, tc.lifetime, poolCfg.MaxConnLifetime)
			require.Equal(t, tc.idleTime, poolCfg.MaxConnIdleTime)
			query, locks, err := cfg.CoreBudgets()
			if tc.maxConns < 2 {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.maxConns, query.MaxConns+locks.MaxConns)
			require.Equal(t, 1, locks.MaxConns)
			require.NoError(t, query.Validate(1))
			require.NoError(t, locks.Validate(1))
		})
	}
}

func TestParsePoolBootstrapSettings(t *testing.T) {
	for _, prefix := range []string{"CORE", "SSO"} {
		t.Run(prefix, func(t *testing.T) {
			values := map[string]string{prefix + "_DB_MAX_OPEN": "2", prefix + "_DB_MAX_LIFETIME": "20m", prefix + "_DB_MAX_IDLE_TIME": "1m"}
			lookup := func(key string) (string, bool) { value, ok := values[key]; return value, ok }
			cfg, err := pgpool.Parse(lookup, prefix)
			require.NoError(t, err)
			require.Equal(t, pgpool.Config{MaxConns: 2, MaxConnLifetime: 20 * time.Minute, MaxConnIdleTime: time.Minute}, cfg)
			for _, suffix := range []string{"MAX_OPEN", "MAX_LIFETIME", "MAX_IDLE_TIME"} {
				key := prefix + "_DB_" + suffix
				previous := values[key]
				values[key] = "secret-invalid-value"
				_, err := pgpool.Parse(lookup, prefix)
				require.ErrorContains(t, err, key)
				require.NotContains(t, err.Error(), "secret-invalid-value")
				if suffix == "MAX_OPEN" {
					require.ErrorIs(t, err, strconv.ErrSyntax)
				}
				values[key] = previous
			}
		})
	}
	cfg, err := pgpool.Parse(func(string) (string, bool) { return "", false }, "CORE")
	require.NoError(t, err)
	require.Equal(t, pgpool.DefaultConfig(), cfg)
}

func TestPoolDiagnosticsCancelWithoutDatabaseAccess(t *testing.T) {
	poolCfg, err := pgxpool.ParseConfig("postgres://user:secret-password@127.0.0.1:1/db")
	require.NoError(t, err)
	require.NoError(t, pgpool.DefaultConfig().Apply(poolCfg))
	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	require.NoError(t, err)
	defer pool.Close()
	var output strings.Builder
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pgpool.LogStats(ctx, logger, map[string]*pgxpool.Pool{"queries": pool})
	require.Contains(t, output.String(), `"pool":"queries"`)
	require.Contains(t, output.String(), `"max_conns":20`)
	require.Contains(t, output.String(), `"empty_acquire_count":0`)
	require.NotContains(t, output.String(), "secret-password")
	require.Zero(t, pool.Stat().TotalConns())
}

func TestNewPoolConfigAppliesBudgetAndKeepsDSNOutOfErrors(t *testing.T) {
	budget := pgpool.Config{MaxConns: 3, MaxConnLifetime: time.Minute, MaxConnIdleTime: time.Second}
	poolCfg, err := pgpool.NewPoolConfig("postgres://user:pw@127.0.0.1:1/db", budget)
	require.NoError(t, err)
	require.EqualValues(t, 3, poolCfg.MaxConns)
	require.NotNil(t, poolCfg.AfterConnect, "timestamptz scans must be pinned to UTC on every connection")

	_, err = pgpool.NewPoolConfig("postgres://user:secret-pw@[bad", budget)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret-pw")

	_, err = pgpool.NewPoolConfig("postgres://127.0.0.1:1/db", pgpool.Config{})
	require.Error(t, err)
}
