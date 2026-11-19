package repository

import (
	"fmt"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// claimedBefore is the recovery statements' age bound on column: with the
// parameter at zero it matches every row, as startup recovery wants;
// otherwise only rows last updated longer ago than that many seconds, on the
// database clock.
func claimedBefore(column string, param int) string {
	return fmt.Sprintf("($%d::double precision = 0 OR %s)", param, updatedBefore(column, param))
}

// updatedBefore matches rows whose column is older than the parameter's
// number of seconds, on the database clock.
func updatedBefore(column string, param int) string {
	return fmt.Sprintf("%s < NOW() - make_interval(secs => $%d::double precision)", column, param)
}

// ageSeconds passes an age bound to a statement; SQL holds no business
// durations.
func ageSeconds(age time.Duration) float64 {
	return max(age, 0).Seconds()
}

// collectRecoveredOperations reads rows of id, resource_type, resource_id and
// operation, and closes them.
func collectRecoveredOperations(rows pgx.Rows) ([]model.RecoveredOperation, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.RecoveredOperation, error) {
		var op model.RecoveredOperation
		err := row.Scan(&op.ID, &op.ResourceType, &op.ResourceID, &op.Operation)
		return op, err
	})
}

func recoveredIDs(ops []model.RecoveredOperation) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(ops))
	for _, op := range ops {
		ids = append(ids, op.ID)
	}
	return ids
}
