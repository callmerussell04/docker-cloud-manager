// Package pgpool owns bootstrap PostgreSQL connection budgets, pool
// construction and diagnostics.
package pgpool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
)

// Config is a pgxpool connection budget.
type Config struct {
	MaxConns        int
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

func DefaultConfig() Config {
	return Config{MaxConns: 20, MaxConnLifetime: 30 * time.Minute, MaxConnIdleTime: 5 * time.Minute}
}

// Parse reads service-prefixed bootstrap settings through the entrypoint's lookup.
// Invalid values fail startup rather than silently selecting an unlimited pool.
// Errors name the setting but never echo its raw value, so only value-free
// causes are wrapped.
func Parse(lookup envconfig.Lookup, prefix string) (Config, error) {
	cfg := DefaultConfig()
	maxOpenKey := prefix + "_DB_MAX_OPEN"
	if raw, ok := lookup(maxOpenKey); ok {
		value, err := strconv.Atoi(raw)
		if err != nil {
			// *strconv.NumError quotes the raw value; its unwrapped ErrSyntax or ErrRange does not.
			return Config{}, fmt.Errorf("invalid %s: %w", maxOpenKey, errors.Unwrap(err))
		}
		cfg.MaxConns = value
	}
	for suffix, target := range map[string]*time.Duration{"MAX_LIFETIME": &cfg.MaxConnLifetime, "MAX_IDLE_TIME": &cfg.MaxConnIdleTime} {
		key := prefix + "_DB_" + suffix
		if raw, ok := lookup(key); ok {
			value, err := time.ParseDuration(raw)
			if err != nil {
				// time.ParseDuration has no value-free cause; its message quotes the raw value.
				return Config{}, fmt.Errorf("invalid %s", key)
			}
			*target = value
		}
	}
	return cfg, nil
}

func (c Config) Validate(minMaxConns int) error {
	if c.MaxConns < minMaxConns || c.MaxConns > math.MaxInt32 || c.MaxConnLifetime <= 0 || c.MaxConnIdleTime <= 0 {
		return fmt.Errorf("invalid postgres pool budget: require max conns >= %d and positive lifetimes", minMaxConns)
	}
	return nil
}

// Apply writes the budget onto a pool config. pgxpool cannot be resized after
// construction, so the budget is applied before NewWithConfig.
func (c Config) Apply(poolCfg *pgxpool.Config) error {
	if err := c.Validate(1); err != nil {
		return err
	}
	poolCfg.MaxConns = int32(c.MaxConns) // bounded by Validate
	poolCfg.MaxConnLifetime = c.MaxConnLifetime
	poolCfg.MaxConnIdleTime = c.MaxConnIdleTime
	return nil
}

// NewPoolConfig parses dsn and applies the budget. Every connection scans
// timestamptz values in UTC, as the previous driver did with the servers'
// UTC session time zone, so persisted instants never pick up the host's zone.
// Errors never include the DSN.
func NewPoolConfig(dsn string, budget Config) (*pgxpool.Config, error) {
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		// pgconn's parse error quotes the connection string.
		return nil, errors.New("parse postgres connection settings")
	}
	if err := budget.Apply(poolCfg); err != nil {
		return nil, err
	}
	poolCfg.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
		conn.TypeMap().RegisterType(&pgtype.Type{
			Name:  "timestamptz",
			OID:   pgtype.TimestamptzOID,
			Codec: &pgtype.TimestamptzCodec{ScanLocation: time.UTC},
		})
		return nil
	}
	return poolCfg, nil
}

// PingTimeout bounds the startup ping of a pool.
const PingTimeout = 10 * time.Second

// Open builds a pool from dsn with the budget applied and pings it within
// pingTimeout. Errors never include the DSN. On success the caller closes the pool.
func Open(ctx context.Context, dsn string, budget Config, pingTimeout time.Duration) (*pgxpool.Pool, error) {
	poolCfg, err := NewPoolConfig(dsn, budget)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres pool: %w", err)
	}
	return pool, nil
}

// Core reserves one connection for pinned advisory-lock transactions and waiters.
// Lock holders can always use the independently bounded query pool.
func (c Config) CoreBudgets() (Config, Config, error) {
	if err := c.Validate(2); err != nil {
		return Config{}, Config{}, err
	}
	query, locks := c, c
	locks.MaxConns = 1
	query.MaxConns--
	return query, locks, nil
}

// LogStats uses the existing structured logging seam and never includes a DSN.
func LogStats(ctx context.Context, logger *slog.Logger, pools map[string]*pgxpool.Pool) {
	log := func() {
		for name, pool := range pools {
			s := pool.Stat()
			logger.Info("postgres pool statistics", "pool", name, "max_conns", s.MaxConns(),
				"total", s.TotalConns(), "acquired", s.AcquiredConns(), "idle", s.IdleConns(),
				"constructing", s.ConstructingConns(), "acquire_count", s.AcquireCount(),
				"empty_acquire_count", s.EmptyAcquireCount(), "canceled_acquire_count", s.CanceledAcquireCount(),
				"acquire_duration_ms", s.AcquireDuration().Milliseconds(),
				"max_idle_destroy_count", s.MaxIdleDestroyCount(), "max_lifetime_destroy_count", s.MaxLifetimeDestroyCount())
		}
	}
	log()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			log()
		}
	}
}
