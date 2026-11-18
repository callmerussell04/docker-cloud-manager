package ssotest_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/ssotest"
)

func TestLoginAttemptsFakeMeetsTheStoreContract(t *testing.T) {
	ssotest.RunLoginAttemptContract(t, func(*testing.T) service.LoginAttemptRepository {
		return ssotest.NewLoginAttempts()
	})
}
