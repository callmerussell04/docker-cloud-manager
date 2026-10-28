package repository

import (
	"database/sql"
	"fmt"
	"sync"
)

// commitTx commits tx and names the repository operation on failure while
// preserving the driver error for errors.Is/As.
func commitTx(tx *sql.Tx, operation string) error {
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", operation, err)
	}
	return nil
}

// lockRelease returns a function that rolls back a transaction holding
// advisory transaction locks, which releases them. It runs at most once, so a
// release on an error path and a deferred caller release cannot race. A failed
// rollback ends the session, which releases the locks too.
func lockRelease(tx *sql.Tx) func() {
	var once sync.Once
	return func() { once.Do(func() { _ = tx.Rollback() }) }
}

// wrapOp names the repository operation that failed while preserving the cause
// for errors.Is/As (nil stays nil), so transports still map it by kind and expose only safe text.
func wrapOp(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
