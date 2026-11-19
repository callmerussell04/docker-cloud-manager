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

// The resumable-kind rule: recovery returns a container create, a volume
// create or delete and an image delete to the queue, and fails every other
// interrupted operation. These three helpers are the one place it is
// written; each matches on resource_operations aliased op, with parameters
// numbered from first.

// resumableContainerOperation matches the container operation recovery
// returns to the queue: a create.
func resumableContainerOperation(first int) string {
	return fmt.Sprintf(`(op.resource_type = $%d AND op.operation = $%d)`, first, first+1)
}

func resumableContainerOperationArgs() []any {
	return []any{model.ResourceTypeContainer, model.OperationCreate}
}

// resumableResourceOperation matches the volume and image operations
// recovery returns to the queue: volume create and delete, and image delete.
func resumableResourceOperation(first int) string {
	return fmt.Sprintf(`((op.resource_type = $%[1]d AND op.operation IN ($%[2]d, $%[3]d)) OR (op.resource_type = $%[4]d AND op.operation = $%[3]d))`,
		first, first+1, first+2, first+3)
}

func resumableResourceOperationArgs() []any {
	return []any{model.ResourceTypeVolume, model.OperationCreate, model.OperationDelete, model.ResourceTypeImage}
}

// resumableOperation matches every operation recovery returns to the queue.
func resumableOperation(first int) string {
	return "(" + resumableContainerOperation(first) + " OR " + resumableResourceOperation(first+2) + ")"
}

func resumableOperationArgs() []any {
	return append(resumableContainerOperationArgs(), resumableResourceOperationArgs()...)
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
