package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSSOMigrationsApply(t *testing.T) {
	db := dbtest.OpenSSOPostgres(t)

	require.GreaterOrEqual(t, dbtest.CountRows(t, db, "users"), 0)
}

func TestUserRepositorySaveGetUpdateAndList(t *testing.T) {
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
	require.NoError(t, repo.UpdateUser(ctx, local))
	updated, err := repo.GetUserByID(ctx, local.ID)
	require.NoError(t, err)
	require.Equal(t, "alice2", updated.Username)

	listed, total, err := repo.ListUsers(ctx, model.ListUsersOptions{Limit: 10, Offset: 0})
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, listed, 2)
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
		AuthSource:   model.AuthSourceLocal,
	}
}

func TestUserRepositoryTreatsUsernamesAndEmailsAsCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenSSOPostgres(t)
	repo := repository.NewUserRepository(db)
	alice := user("alice", model.RoleUser, model.StatusActive)
	require.NoError(t, repo.SaveUser(ctx, alice))

	sameName := user("Alice", model.RoleUser, model.StatusActive)
	sameName.Email = "other@example.com"
	require.ErrorIs(t, repo.SaveUser(ctx, sameName), apperrors.ErrAlreadyExists)

	sameEmail := user("bob", model.RoleUser, model.StatusActive)
	sameEmail.Email = "ALICE@Example.com"
	require.ErrorIs(t, repo.SaveUser(ctx, sameEmail), apperrors.ErrAlreadyExists)

	byUsername, err := repo.GetUserByUsername(ctx, "ALICE")
	require.NoError(t, err)
	require.Equal(t, alice.ID, byUsername.ID)
	byEmail, err := repo.GetUserByEmail(ctx, "Alice@EXAMPLE.com")
	require.NoError(t, err)
	require.Equal(t, alice.ID, byEmail.ID)

	bob := user("bob", model.RoleUser, model.StatusActive)
	require.NoError(t, repo.SaveUser(ctx, bob))
	bob.Username = "ALICE"
	require.ErrorIs(t, repo.UpdateUser(ctx, bob), apperrors.ErrAlreadyExists)
}
