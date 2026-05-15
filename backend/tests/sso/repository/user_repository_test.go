package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/repository"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSSOMigrationsApply(t *testing.T) {
	db := dbtest.OpenSSOPostgres(t)

	require.GreaterOrEqual(t, dbtest.CountRows(t, db, "users"), 0)
}

func TestUserRepositorySaveGetUpdateListAndCount(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenSSOPostgres(t)
	repo := repository.NewUserRepository(db)
	lastLoginAt := time.Now().UTC().Truncate(time.Microsecond)
	local := user("alice", model.RoleAdmin, model.StatusActive)
	oidc := user("oidc-user", model.RoleUser, model.StatusActive)
	oidc.PasswordHash = ""
	oidc.AuthSource = model.AuthSourceOIDC
	oidc.ExternalProvider = "keycloak"
	oidc.ExternalSubject = "subject-1"
	oidc.ExternalUsername = "alice.external"
	oidc.LastLoginAt = &lastLoginAt

	require.NoError(t, repo.SaveUser(ctx, local))
	require.NoError(t, repo.SaveUser(ctx, oidc))

	byUsername, err := repo.GetUserByUsername(ctx, "alice")
	require.NoError(t, err)
	require.Equal(t, local.ID, byUsername.ID)
	require.Equal(t, model.AuthSourceLocal, byUsername.AuthSource)

	byEmail, err := repo.GetUserByEmail(ctx, oidc.Email)
	require.NoError(t, err)
	require.Equal(t, oidc.ID, byEmail.ID)
	require.Equal(t, model.AuthSourceOIDC, byEmail.AuthSource)
	require.Equal(t, "keycloak", byEmail.ExternalProvider)
	require.NotNil(t, byEmail.LastLoginAt)

	byID, err := repo.GetUserByID(ctx, local.ID)
	require.NoError(t, err)
	require.Equal(t, "alice", byID.Username)

	byExternal, err := repo.GetUserByExternalIdentity(ctx, "keycloak", "subject-1")
	require.NoError(t, err)
	require.Equal(t, oidc.ID, byExternal.ID)

	users, err := repo.GetUsersByIDs(ctx, []uuid.UUID{local.ID, oidc.ID})
	require.NoError(t, err)
	require.Len(t, users, 2)

	emptyUsers, err := repo.GetUsersByIDs(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, emptyUsers)

	local.Username = "alice2"
	local.Email = "alice2@example.com"
	local.QuotaCPU = 2
	local.QuotaRAMMB = 4096
	local.QuotaDiskMB = 8192
	require.NoError(t, repo.UpdateUser(ctx, local))
	updated, err := repo.GetUserByID(ctx, local.ID)
	require.NoError(t, err)
	require.Equal(t, "alice2", updated.Username)
	require.Equal(t, float64(2), updated.QuotaCPU)
	require.Equal(t, int64(8192), updated.QuotaDiskMB)

	listed, total, err := repo.ListUsers(ctx, model.ListUsersOptions{Limit: 10, Offset: 0})
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, listed, 2)

	admins, err := repo.CountActiveAdmins(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, admins)
}

func TestUserRepositoryMapsConstraintAndNotFoundErrors(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenSSOPostgres(t)
	repo := repository.NewUserRepository(db)

	alice := user("alice", model.RoleUser, model.StatusActive)
	require.NoError(t, repo.SaveUser(ctx, alice))

	duplicateUsername := user("alice", model.RoleUser, model.StatusActive)
	duplicateUsername.Email = "alice2@example.com"
	require.ErrorIs(t, repo.SaveUser(ctx, duplicateUsername), apperrors.ErrAlreadyExists)

	duplicateEmail := user("alice2", model.RoleUser, model.StatusActive)
	duplicateEmail.Email = alice.Email
	require.ErrorIs(t, repo.SaveUser(ctx, duplicateEmail), apperrors.ErrAlreadyExists)

	oidc := user("oidc-user", model.RoleUser, model.StatusActive)
	oidc.PasswordHash = ""
	oidc.AuthSource = model.AuthSourceOIDC
	oidc.ExternalProvider = "keycloak"
	oidc.ExternalSubject = "subject-1"
	require.NoError(t, repo.SaveUser(ctx, oidc))

	duplicateExternal := user("oidc-user-2", model.RoleUser, model.StatusActive)
	duplicateExternal.PasswordHash = ""
	duplicateExternal.AuthSource = model.AuthSourceOIDC
	duplicateExternal.ExternalProvider = "keycloak"
	duplicateExternal.ExternalSubject = "subject-1"
	require.ErrorIs(t, repo.SaveUser(ctx, duplicateExternal), apperrors.ErrAlreadyExists)

	_, err := repo.GetUserByID(ctx, uuid.New())
	require.ErrorIs(t, err, apperrors.ErrNotFound)

	missing := user("missing", model.RoleUser, model.StatusActive)
	require.ErrorIs(t, repo.UpdateUser(ctx, missing), apperrors.ErrNotFound)
}

func user(username, role, status string) model.User {
	return model.User{
		ID:           uuid.New(),
		Username:     username,
		Email:        username + "@example.com",
		PasswordHash: "hash-" + username,
		Role:         role,
		Status:       status,
		QuotaCPU:     1,
		QuotaRAMMB:   2048,
		QuotaDiskMB:  5120,
		AuthSource:   model.AuthSourceLocal,
	}
}
