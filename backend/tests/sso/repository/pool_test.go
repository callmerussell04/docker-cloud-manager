package repository_test

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/platform/pgpool"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/repository"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/dbtest"
	"github.com/stretchr/testify/require"
)

func TestSSOQueriesStayBoundedAndCancel(t *testing.T) {
	db := dbtest.OpenSSOPostgres(t)
	peer := dbtest.OpenSSOPeerPool(t, db)
	cfg := pgpool.DefaultConfig()
	cfg.MaxOpen, cfg.MaxIdle = 1, 0
	require.NoError(t, cfg.Apply(db))
	repo := repository.NewUserRepository(db)
	dbtest.CheckPoolProgress(t, db, peer, "users", func(ctx context.Context) error { _, err := repo.CountActiveAdmins(ctx); return err })
}
