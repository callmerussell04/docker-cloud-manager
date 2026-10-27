package repository

import (
	"database/sql"
	"fmt"
)

// commitTx commits tx and names the repository operation on failure while
// preserving the driver error for errors.Is/As.
func commitTx(tx *sql.Tx, operation string) error {
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", operation, err)
	}
	return nil
}

// wrapOp names the repository operation that failed while preserving the cause
// for errors.Is/As (nil stays nil), so transports still map it by kind and expose only safe text.
func wrapOp(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
