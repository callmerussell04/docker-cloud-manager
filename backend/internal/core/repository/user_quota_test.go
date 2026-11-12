package repository_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
)

func TestUserQuotaRepositoryContract(t *testing.T) {
	coretest.RunUserQuotaStoreContract(t, func(t *testing.T) coretest.UserQuotaStore {
		return repository.NewUserQuotaRepository(dbtest.OpenCorePostgres(t))
	})
}
