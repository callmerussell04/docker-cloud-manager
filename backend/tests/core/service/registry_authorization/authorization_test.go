package registry_authorization_test

import (
	"context"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestBuildCredentialCannotWidenRepositoryAuthority(t *testing.T) {
	owner := uuid.New()
	repo := coremocks.NewRegistryGrantRepository(t)
	users := coremocks.NewUserInfoProvider(t)
	signer := coremocks.NewRegistryTokenSigner(t)
	principal := model.RegistryPrincipal{BuildID: uuid.New(), OwnerID: owner, Repository: "a_destination", PullRepositories: []string{"a_base"}}
	repo.EXPECT().AuthorizeCredential(mock.Anything, mock.Anything, mock.Anything).Return(principal, nil)
	users.EXPECT().GetUser(mock.Anything, owner).Return(model.UserInfo{ID: owner, Status: "active"}, nil)
	signer.EXPECT().SignRegistryToken(mock.Anything, mock.MatchedBy(func(g model.RegistryTokenGrant) bool {
		require.Equal(t, []model.RegistryAccess{{Repository: "a_destination", Actions: []string{"pull", "push"}}, {Repository: "a_base", Actions: []string{"pull"}}}, g.Access)
		require.WithinDuration(t, time.Now().Add(time.Minute), g.ExpiresAt, time.Second)
		return true
	})).Return("scoped-token", nil)
	auth := newRegistryAuthorization(t, repo, users, signer, "registry.test:5000")
	token, err := auth.Exchange(context.Background(), principal.BuildID.String(), "job-secret", "dcm-registry", []string{"repository:a_destination:pull,push,delete", "repository:b_private:pull,push,delete", "registry:catalog:*", "repository:a_base:pull,push", "repository:*:*"})
	require.NoError(t, err)
	require.Equal(t, "scoped-token", token)
}

func TestDeactivatedOwnerCannotExchangeBuildCredential(t *testing.T) {
	owner, id := uuid.New(), uuid.New()
	repo := coremocks.NewRegistryGrantRepository(t)
	users := coremocks.NewUserInfoProvider(t)
	signer := coremocks.NewRegistryTokenSigner(t)
	repo.EXPECT().AuthorizeCredential(mock.Anything, mock.Anything, mock.Anything).Return(model.RegistryPrincipal{BuildID: id, OwnerID: owner}, nil)
	users.EXPECT().GetUser(mock.Anything, owner).Return(model.UserInfo{ID: owner, Status: "deactivated"}, nil)
	auth := newRegistryAuthorization(t, repo, users, signer, "registry:5000")
	_, err := auth.Exchange(context.Background(), id.String(), "secret", service.RegistryService, nil)
	require.ErrorIs(t, err, apperrors.ErrForbidden)
}

func TestExpiredOrRevokedCredentialFailsWithoutSigning(t *testing.T) {
	repo := coremocks.NewRegistryGrantRepository(t)
	users := coremocks.NewUserInfoProvider(t)
	signer := coremocks.NewRegistryTokenSigner(t)
	repo.EXPECT().AuthorizeCredential(mock.Anything, mock.Anything, mock.Anything).Return(model.RegistryPrincipal{}, apperrors.ErrForbidden)
	auth := newRegistryAuthorization(t, repo, users, signer, "registry:5000")
	_, err := auth.Exchange(context.Background(), uuid.NewString(), "secret", service.RegistryService, nil)
	require.ErrorIs(t, err, apperrors.ErrForbidden)
}

func TestImageDeletionDoesNotRevokeAnActiveSiblingBuild(t *testing.T) {
	repo := coremocks.NewRegistryGrantRepository(t)
	users := coremocks.NewUserInfoProvider(t)
	signer := coremocks.NewRegistryTokenSigner(t)
	repo.EXPECT().HasActiveRegistryBuilds(mock.Anything, "owner_repository").Return(true, nil)
	auth := newRegistryAuthorization(t, repo, users, signer, "registry:5000")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	unlock, err := auth.FenceAndLockRepository(ctx, "owner_repository")
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, unlock)
	repo.AssertNotCalled(t, "FenceRepository", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "LockRegistryRepository", mock.Anything, mock.Anything)
}

func TestTrustedTokenGrantsOnlyTheActionsOfItsAccess(t *testing.T) {
	tests := []struct {
		name            string
		access          model.RegistryTrustedAccess
		expectedSubject string
		expectedActions []string
	}{
		{name: "pull", access: model.RegistryTrustedPull, expectedSubject: "core-pull", expectedActions: []string{"pull"}},
		{name: "cleanup", access: model.RegistryTrustedCleanup, expectedSubject: "core-cleanup", expectedActions: []string{"pull", "delete"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := coremocks.NewRegistryGrantRepository(t)
			signer := coremocks.NewRegistryTokenSigner(t)
			repo.EXPECT().RepositoryExists(mock.Anything, "owner_app").Return(true, nil)
			signer.EXPECT().SignRegistryToken(mock.Anything, mock.MatchedBy(func(g model.RegistryTokenGrant) bool {
				return g.Subject == tc.expectedSubject && g.Service == service.RegistryService &&
					assert.ObjectsAreEqual([]model.RegistryAccess{{Repository: "owner_app", Actions: tc.expectedActions}}, g.Access)
			})).Return("trusted-token", nil)
			auth := newRegistryAuthorization(t, repo, nil, signer, "registry:5000")
			token, err := auth.TrustedToken(context.Background(), "owner_app", tc.access)
			require.NoError(t, err)
			require.Equal(t, "trusted-token", token)
		})
	}
}

func TestTrustedTokenRejectsUnknownAccessWithoutSigning(t *testing.T) {
	repo := coremocks.NewRegistryGrantRepository(t)
	signer := coremocks.NewRegistryTokenSigner(t)
	auth := newRegistryAuthorization(t, repo, nil, signer, "registry:5000")
	_, err := auth.TrustedToken(context.Background(), "owner_app", model.RegistryTrustedAccess("push"))
	require.ErrorIs(t, err, apperrors.ErrForbidden)
}

func TestRevokeBuildPassesTheTagDispositionToTheRepository(t *testing.T) {
	for _, disposition := range []model.RegistryTagDisposition{model.RegistryTagKeep, model.RegistryTagDelete} {
		t.Run(string(disposition), func(t *testing.T) {
			id := uuid.New()
			repo := coremocks.NewRegistryGrantRepository(t)
			repo.EXPECT().RevokeBuild(mock.Anything, id, disposition, service.RegistryCleanupDelay).Return(nil).Once()
			auth := newRegistryAuthorization(t, repo, nil, nil, "registry:5000")
			require.NoError(t, auth.RevokeBuild(context.Background(), id, disposition))
		})
	}
}

func TestRevokeBuildRejectsUnknownTagDispositionWithoutRevoking(t *testing.T) {
	repo := coremocks.NewRegistryGrantRepository(t)
	auth := newRegistryAuthorization(t, repo, nil, nil, "registry:5000")
	err := auth.RevokeBuild(context.Background(), uuid.New(), model.RegistryTagDisposition("archive"))
	require.ErrorIs(t, err, apperrors.ErrInternal)
}

// newRegistryAuthorization substitutes an unused mock for a nil users or signer
// dependency, so each test states only the collaborators it exercises.
func newRegistryAuthorization(t *testing.T, repo service.RegistryGrantRepository, users *coremocks.UserInfoProvider, signer *coremocks.RegistryTokenSigner, authority string) *service.RegistryAuthorization {
	t.Helper()
	if users == nil {
		users = coremocks.NewUserInfoProvider(t)
	}
	if signer == nil {
		signer = coremocks.NewRegistryTokenSigner(t)
	}
	auth, err := service.NewRegistryAuthorization(service.RegistryAuthorizationDeps{Repository: repo, Users: users, Signer: signer, Authority: authority})
	require.NoError(t, err)
	return auth
}

func TestRegistryAuthorizationRejectsMissingDependencies(t *testing.T) {
	valid := func() service.RegistryAuthorizationDeps {
		return service.RegistryAuthorizationDeps{
			Repository: coremocks.NewRegistryGrantRepository(t),
			Users:      coremocks.NewUserInfoProvider(t),
			Signer:     coremocks.NewRegistryTokenSigner(t),
			Authority:  "registry:5000",
		}
	}
	for _, tc := range []struct {
		name    string
		mutate  func(*service.RegistryAuthorizationDeps)
		message string
	}{
		{"repository", func(d *service.RegistryAuthorizationDeps) { d.Repository = nil }, "repository is required"},
		{"typed nil repository", func(d *service.RegistryAuthorizationDeps) { d.Repository = (*coremocks.RegistryGrantRepository)(nil) }, "repository is required"},
		{"users", func(d *service.RegistryAuthorizationDeps) { d.Users = nil }, "users is required"},
		{"typed nil users", func(d *service.RegistryAuthorizationDeps) { d.Users = (*coremocks.UserInfoProvider)(nil) }, "users is required"},
		{"signer", func(d *service.RegistryAuthorizationDeps) { d.Signer = nil }, "signer is required"},
		{"typed nil signer", func(d *service.RegistryAuthorizationDeps) { d.Signer = (*coremocks.RegistryTokenSigner)(nil) }, "signer is required"},
		{"authority", func(d *service.RegistryAuthorizationDeps) { d.Authority = "" }, "authority is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := valid()
			tc.mutate(&deps)
			auth, err := service.NewRegistryAuthorization(deps)
			require.ErrorContains(t, err, tc.message)
			require.Nil(t, auth)
		})
	}
}
