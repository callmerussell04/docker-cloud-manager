package repository_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
)

func TestContainerRepositoryContract(t *testing.T) {
	coretest.RunContainerStoreContract(t, func(t *testing.T) coretest.ContainerStore {
		db := dbtest.OpenCorePostgres(t)
		return repository.NewContainerRepository(db, db)
	})
}
