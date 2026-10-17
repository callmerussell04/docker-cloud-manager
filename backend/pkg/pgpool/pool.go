// Package pgpool owns bootstrap PostgreSQL connection budgets and diagnostics.
package pgpool

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"time"
)

type Config struct {
	MaxOpen     int
	MaxIdle     int
	MaxLifetime time.Duration
	MaxIdleTime time.Duration
}

func DefaultConfig() Config {
	return Config{MaxOpen: 20, MaxIdle: 5, MaxLifetime: 30 * time.Minute, MaxIdleTime: 5 * time.Minute}
}

// Parse reads service-prefixed bootstrap settings through the entrypoint's lookup.
// Invalid values fail startup rather than silently selecting an unlimited pool.
func Parse(lookup func(string) (string, bool), prefix string) (Config, error) {
	cfg := DefaultConfig()
	for suffix, target := range map[string]*int{"MAX_OPEN": &cfg.MaxOpen, "MAX_IDLE": &cfg.MaxIdle} {
		key := prefix + "_DB_" + suffix
		if raw, ok := lookup(key); ok {
			value, err := strconv.Atoi(raw)
			if err != nil {
				return Config{}, fmt.Errorf("invalid %s", key)
			}
			*target = value
		}
	}
	for suffix, target := range map[string]*time.Duration{"MAX_LIFETIME": &cfg.MaxLifetime, "MAX_IDLE_TIME": &cfg.MaxIdleTime} {
		key := prefix + "_DB_" + suffix
		if raw, ok := lookup(key); ok {
			value, err := time.ParseDuration(raw)
			if err != nil {
				return Config{}, fmt.Errorf("invalid %s", key)
			}
			*target = value
		}
	}
	return cfg, nil
}

func (c Config) Validate(minOpen int) error {
	if c.MaxOpen < minOpen || c.MaxIdle < 0 || c.MaxIdle > c.MaxOpen || c.MaxLifetime <= 0 || c.MaxIdleTime <= 0 {
		return fmt.Errorf("invalid postgres pool budget: require max open >= %d, idle between zero and max open, and positive lifetimes", minOpen)
	}
	return nil
}

func (c Config) Apply(db *sql.DB) error {
	if err := c.Validate(1); err != nil {
		return err
	}
	db.SetMaxOpenConns(c.MaxOpen)
	db.SetMaxIdleConns(c.MaxIdle)
	db.SetConnMaxLifetime(c.MaxLifetime)
	db.SetConnMaxIdleTime(c.MaxIdleTime)
	return nil
}

// Core reserves one connection for pinned advisory-lock transactions and waiters.
// Lock holders can always use the independently bounded query pool.
func (c Config) CoreBudgets() (Config, Config, error) {
	if err := c.Validate(2); err != nil {
		return Config{}, Config{}, err
	}
	query, locks := c, c
	locks.MaxOpen = 1
	locks.MaxIdle = min(1, c.MaxIdle)
	query.MaxOpen--
	query.MaxIdle -= locks.MaxIdle
	return query, locks, nil
}

// LogStats uses the existing structured logging seam and never includes a DSN.
func LogStats(ctx context.Context, logger *slog.Logger, pools map[string]*sql.DB) {
	log := func() {
		for name, db := range pools {
			s := db.Stats()
			logger.Info("postgres pool statistics", "pool", name, "max_open", s.MaxOpenConnections,
				"open", s.OpenConnections, "in_use", s.InUse, "idle", s.Idle,
				"wait_count", s.WaitCount, "wait_duration_ms", s.WaitDuration.Milliseconds(),
				"max_idle_closed", s.MaxIdleClosed, "max_idle_time_closed", s.MaxIdleTimeClosed, "max_lifetime_closed", s.MaxLifetimeClosed)
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
