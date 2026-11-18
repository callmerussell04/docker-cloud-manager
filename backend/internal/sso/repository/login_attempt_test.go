package repository_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/ssotest"
)

func TestLoginAttemptRepositoryMeetsTheStoreContract(t *testing.T) {
	ssotest.RunLoginAttemptContract(t, func(t *testing.T) service.LoginAttemptRepository {
		return repository.NewLoginAttemptRepository(dbtest.OpenSSOPostgres(t))
	})
}
