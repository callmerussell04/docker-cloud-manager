package repository_test

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
)

func TestSSOQueriesStayBoundedAndCancel(t *testing.T) {
	peer := dbtest.OpenSSOPostgres(t)
	db := dbtest.OpenSSOPeerPool(t, peer, dbtest.Budget(1))
	repo := repository.NewUserRepository(db)
	dbtest.CheckPoolProgress(t, db, peer, "users", func(ctx context.Context) error {
		_, _, err := repo.ListUsers(ctx, model.ListUsersOptions{Limit: 1})
		return err
	})
}
