package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	gatewayservicemocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/gateway/service"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var (
	alice = model.AdminUser{UserID: "6f1e2a1c-0000-4000-8000-000000000001", Username: "alice", Email: "alice@example.com", Role: "user", Status: "active"}
	bob   = model.AdminUser{UserID: "6f1e2a1c-0000-4000-8000-000000000002", Username: "bob", Email: "bob@example.com", Role: "admin", Status: "active"}
)

func newAdminUsers(t *testing.T) (*service.AdminUsers, *gatewayservicemocks.MockAdminUserDirectory, *gatewayservicemocks.MockUserQuotaAPI) {
	t.Helper()
	users := gatewayservicemocks.NewMockAdminUserDirectory(t)
	quotas := gatewayservicemocks.NewMockUserQuotaAPI(t)
	return service.NewAdminUsers(users, quotas), users, quotas
}

// One Core call fills the quotas of a whole page.
func TestListUsersCombinesAPageWithItsQuotas(t *testing.T) {
	adminUsers, users, quotas := newAdminUsers(t)
	users.EXPECT().ListUsers(mock.Anything, 2, 10).Return(model.PaginatedAdminUsers{Users: []model.AdminUser{alice, bob}, TotalCount: 12}, nil)
	aliceQuota := model.UserQuota{UserID: alice.UserID, CPU: 2.5, RAMMB: 4096, DiskMB: 10240, Custom: true}
	bobQuota := model.UserQuota{UserID: bob.UserID, CPU: 1, RAMMB: 2048, DiskMB: 5120}
	quotas.EXPECT().GetUserQuotas(mock.Anything, []string{alice.UserID, bob.UserID}).Return([]model.UserQuota{bobQuota, aliceQuota}, nil).Once()

	page, err := adminUsers.ListUsers(context.Background(), 2, 10)

	require.NoError(t, err)
	require.Equal(t, model.PaginatedAdminUsersWithQuota{
		Users:      []model.AdminUserWithQuota{{User: alice, Quota: aliceQuota}, {User: bob, Quota: bobQuota}},
		TotalCount: 12,
	}, page)
}

func TestListUsersOfAnEmptyPageAsksCoreNothing(t *testing.T) {
	adminUsers, users, _ := newAdminUsers(t)
	users.EXPECT().ListUsers(mock.Anything, 9, 10).Return(model.PaginatedAdminUsers{TotalCount: 3}, nil)

	page, err := adminUsers.ListUsers(context.Background(), 9, 10)

	require.NoError(t, err)
	require.Equal(t, model.PaginatedAdminUsersWithQuota{Users: []model.AdminUserWithQuota{}, TotalCount: 3}, page)
}

func TestGetUserCombinesTheUserWithTheirQuota(t *testing.T) {
	adminUsers, users, quotas := newAdminUsers(t)
	users.EXPECT().GetUser(mock.Anything, alice.UserID).Return(alice, nil)
	aliceQuota := model.UserQuota{UserID: alice.UserID, CPU: 1, RAMMB: 2048, DiskMB: 5120}
	quotas.EXPECT().GetUserQuotas(mock.Anything, []string{alice.UserID}).Return([]model.UserQuota{aliceQuota}, nil)

	got, err := adminUsers.GetUser(context.Background(), alice.UserID)

	require.NoError(t, err)
	require.Equal(t, model.AdminUserWithQuota{User: alice, Quota: aliceQuota}, got)
}

// A quota is never guessed: when Core cannot answer, or answers without a
// user's quota, the read fails as unavailable.
func TestUserReadsFailAsUnavailableWithoutCoresQuotas(t *testing.T) {
	for name, answer := range map[string]struct {
		quotas []model.UserQuota
		err    error
	}{
		"core unreachable":     {err: apperrors.ErrUnavailable},
		"core timed out":       {err: apperrors.ErrTimeout},
		"core refused":         {err: apperrors.ErrForbidden},
		"core failed":          {err: errors.New("boom")},
		"a quota missing":      {quotas: []model.UserQuota{}},
		"another user's quota": {quotas: []model.UserQuota{{UserID: bob.UserID, CPU: 1, RAMMB: 1, DiskMB: 1}}},
	} {
		t.Run(name, func(t *testing.T) {
			adminUsers, users, quotas := newAdminUsers(t)
			users.EXPECT().ListUsers(mock.Anything, 1, 20).Return(model.PaginatedAdminUsers{Users: []model.AdminUser{alice}, TotalCount: 1}, nil)
			users.EXPECT().GetUser(mock.Anything, alice.UserID).Return(alice, nil)
			quotas.EXPECT().GetUserQuotas(mock.Anything, []string{alice.UserID}).Return(answer.quotas, answer.err).Times(2)

			_, err := adminUsers.ListUsers(context.Background(), 1, 20)
			requireOnlyUnavailable(t, err)
			_, err = adminUsers.GetUser(context.Background(), alice.UserID)
			requireOnlyUnavailable(t, err)
		})
	}
}

func TestGetUserOfAnUnknownUserIsNotFoundWithoutAskingCore(t *testing.T) {
	adminUsers, users, _ := newAdminUsers(t)
	users.EXPECT().GetUser(mock.Anything, alice.UserID).Return(model.AdminUser{}, apperrors.ErrNotFound)

	_, err := adminUsers.GetUser(context.Background(), alice.UserID)

	require.ErrorIs(t, err, apperrors.ErrNotFound)
}

func TestQuotaWritesConfirmTheUserExistsFirst(t *testing.T) {
	limits := model.UserQuotaLimits{CPU: 2, RAMMB: 4096, DiskMB: 8192}
	t.Run("set", func(t *testing.T) {
		adminUsers, users, quotas := newAdminUsers(t)
		users.EXPECT().GetUser(mock.Anything, alice.UserID).Return(alice, nil)
		want := model.UserQuota{UserID: alice.UserID, CPU: 2, RAMMB: 4096, DiskMB: 8192, Custom: true}
		quotas.EXPECT().SetUserQuota(mock.Anything, alice.UserID, limits).Return(want, nil)

		got, err := adminUsers.SetUserQuota(context.Background(), alice.UserID, limits)

		require.NoError(t, err)
		require.Equal(t, want, got)
	})
	t.Run("reset", func(t *testing.T) {
		adminUsers, users, quotas := newAdminUsers(t)
		users.EXPECT().GetUser(mock.Anything, alice.UserID).Return(alice, nil)
		want := model.UserQuota{UserID: alice.UserID, CPU: 1, RAMMB: 2048, DiskMB: 5120}
		quotas.EXPECT().ResetUserQuota(mock.Anything, alice.UserID).Return(want, nil)

		got, err := adminUsers.ResetUserQuota(context.Background(), alice.UserID)

		require.NoError(t, err)
		require.Equal(t, want, got)
	})
	t.Run("an unknown user is not found and nothing is written", func(t *testing.T) {
		adminUsers, users, _ := newAdminUsers(t)
		users.EXPECT().GetUser(mock.Anything, alice.UserID).Return(model.AdminUser{}, apperrors.ErrNotFound).Times(2)

		_, err := adminUsers.SetUserQuota(context.Background(), alice.UserID, limits)
		require.ErrorIs(t, err, apperrors.ErrNotFound)
		_, err = adminUsers.ResetUserQuota(context.Background(), alice.UserID)
		require.ErrorIs(t, err, apperrors.ErrNotFound)
	})
	t.Run("core's refusal reaches the caller", func(t *testing.T) {
		adminUsers, users, quotas := newAdminUsers(t)
		users.EXPECT().GetUser(mock.Anything, alice.UserID).Return(alice, nil)
		rejected := apperrors.New(apperrors.ErrBadRequest, "memory and disk quotas must be positive")
		quotas.EXPECT().SetUserQuota(mock.Anything, alice.UserID, limits).Return(model.UserQuota{}, rejected)

		_, err := adminUsers.SetUserQuota(context.Background(), alice.UserID, limits)

		require.ErrorIs(t, err, apperrors.ErrBadRequest)
	})
}

func requireOnlyUnavailable(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, apperrors.ErrUnavailable)
	for _, other := range []error{apperrors.ErrForbidden, apperrors.ErrTimeout, apperrors.ErrNotFound} {
		require.NotErrorIs(t, err, other)
	}
}

func TestAQuotaReadFailureKeepsItsCauseForTheLogButNotItsKind(t *testing.T) {
	adminUsers, users, quotas := newAdminUsers(t)
	users.EXPECT().GetUser(mock.Anything, alice.UserID).Return(alice, nil)
	quotas.EXPECT().GetUserQuotas(mock.Anything, []string{alice.UserID}).Return(nil, apperrors.New(apperrors.ErrForbidden, "core refused the caller"))

	_, err := adminUsers.GetUser(context.Background(), alice.UserID)

	requireOnlyUnavailable(t, err)
	require.Contains(t, err.Error(), "core refused the caller")
	require.Equal(t, "user quotas are unavailable", apperrors.SafeMessage(err))
}
