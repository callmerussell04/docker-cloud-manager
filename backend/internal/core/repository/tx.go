package repository

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// executor is what both a pool and a transaction provide, so helpers can run
// inside or outside a transaction.
type executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// commitTx commits tx and names the repository operation on failure while
// preserving the driver error for errors.Is/As.
func commitTx(ctx context.Context, tx pgx.Tx, operation string) error {
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s: %w", operation, err)
	}
	return nil
}

// lockRollbackTimeout bounds the rollback that releases advisory locks. It runs
// on its own context because the caller's may already be canceled.
const lockRollbackTimeout = 5 * time.Second

// lockRelease returns a function that rolls back a transaction holding
// advisory transaction locks, which releases them. It runs at most once, so a
// release on an error path and a deferred caller release cannot race. A failed
// rollback closes the connection, which releases the locks too.
func lockRelease(tx pgx.Tx) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), lockRollbackTimeout)
			defer cancel()
			_ = tx.Rollback(ctx) // see above: failure closes the connection and frees the locks
		})
	}
}

// releaseOnDone also runs release when ctx ends, because pgx, unlike
// database/sql, does not end a transaction when its context does. Register it
// only once the locks are held: the rollback must not overlap a statement on
// the same transaction.
func releaseOnDone(ctx context.Context, release func()) func() {
	stop := context.AfterFunc(ctx, release)
	return func() {
		stop()
		release()
	}
}

// wrapOp names the repository operation that failed while preserving the cause
// for errors.Is/As (nil stays nil), so transports still map it by kind and expose only safe text.
func wrapOp(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// ptr returns a pointer to v, for nullable arguments built from values.
func ptr[T any](v T) *T { return &v }

// nullIfZero stores a zero value as NULL.
func nullIfZero[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

// nullTimeIfZero stores an unset time as NULL.
func nullTimeIfZero(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// valueOrZero reads a nullable column into a field whose zero value means NULL.
func valueOrZero[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
