package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestContainerRepositoryContract(t *testing.T) {
	coretest.RunContainerStoreContract(t, func(t *testing.T) coretest.ContainerStore {
		db := dbtest.OpenCorePostgres(t)
		return backdatedContainerRepository{ContainerRepository: repository.NewContainerRepository(db, db), db: db}
	})
}

// backdatedContainerRepository adds the contract's backdating to the
// repository with plain statements on the database clock.
type backdatedContainerRepository struct {
	*repository.ContainerRepository
	db *pgxpool.Pool
}

func (r backdatedContainerRepository) BackdateOperation(ctx context.Context, id uuid.UUID, age time.Duration) error {
	return backdate(ctx, r.db, `UPDATE resource_operations SET updated_at = updated_at - make_interval(secs => $2) WHERE id = $1`, id, age)
}

func (r backdatedContainerRepository) BackdateContainerOutbox(ctx context.Context, operationID uuid.UUID, age time.Duration) error {
	return backdate(ctx, r.db, `UPDATE container_lifecycle_queue_outbox SET updated_at = updated_at - make_interval(secs => $2) WHERE operation_id = $1`, operationID, age)
}

func backdate(ctx context.Context, db *pgxpool.Pool, statement string, id uuid.UUID, age time.Duration) error {
	res, err := db.Exec(ctx, statement, id, age.Seconds())
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}
