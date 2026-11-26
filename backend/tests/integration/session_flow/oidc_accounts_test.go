package session_flow_test

import (
	"context"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/api/sso"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/oidctest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// oidcAccounts is SSO over PostgreSQL with a reachable fake provider, and the
// administrator API as Gateway calls it.
type oidcAccounts struct {
	t     *testing.T
	ctx   context.Context
	svc   *service.AuthService
	idp   *oidctest.Provider
	users sso.UserAPIClient
	root  model.User
}

// newOIDCAccounts starts SSO with a local administrator, root, so an OIDC
// administrator is never the last one unless a test removes root.
func newOIDCAccounts(t *testing.T, adminGroups ...string) *oidcAccounts {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	db := dbtest.OpenSSOPostgres(t)
	idp := oidctest.New(t, oidctest.Options{RedirectURL: "http://gateway.example" + oidcCallbackPath})
	now := time.Now()
	svc := oidcSSO(t, ctx, db, idp, &now, adminGroups...)
	require.NoError(t, svc.EnsureBootstrapAdmin(ctx, service.BootstrapAdminConfig{Username: "root", Email: "root@example.com", Password: "secret123"}))
	root, _, err := svc.ListUsers(ctx, 1, 0)
	require.NoError(t, err)
	conn := serveSSO(t, svc)(internalauth.UnaryClientInterceptor(gatewayCredential))
	return &oidcAccounts{t: t, ctx: ctx, svc: svc, idp: idp, users: sso.NewUserAPIClient(conn), root: root[0]}
}

// signIn starts a sign-in, lets the provider vouch for identity, and completes
// the callback.
func (a *oidcAccounts) signIn(identity oidctest.Identity) model.OIDCCallbackResult {
	a.t.Helper()
	a.idp.SetIdentity(identity)
	started, err := a.svc.StartOIDCLogin(a.ctx, "keycloak", "/")
	require.NoError(a.t, err)
	back := a.idp.SignIn(started.AuthURL)
	result, err := a.svc.CompleteOIDCCallback(a.ctx, "keycloak", back.Query().Get("code"), back.Query().Get("state"), started.StateBinding)
	require.NoError(a.t, err)
	return result
}

// signedIn signs identity in and returns the user its access token verifies as.
func (a *oidcAccounts) signedIn(identity oidctest.Identity) model.User {
	a.t.Helper()
	result := a.signIn(identity)
	require.Equal(a.t, model.OIDCCallbackSuccess, result.Outcome)
	user, err := a.svc.VerifyAccessToken(a.ctx, result.Tokens.AccessToken)
	require.NoError(a.t, err)
	return user
}

// update is an administrator's edit of user through the administrator API.
func (a *oidcAccounts) update(user model.User, edit func(*sso.UpdateUserRequest)) (*sso.UserData, error) {
	a.t.Helper()
	request := &sso.UpdateUserRequest{UserId: user.ID.String(), Username: user.Username, Email: user.Email, Role: user.Role, Status: user.Status}
	edit(request)
	return a.users.UpdateUser(accessscope.WithAdminScope(a.ctx, a.root.ID, a.root.Username, model.RoleAdmin), request)
}

var alice = oidctest.Identity{Subject: "alice-subject", PreferredUsername: "alice", Email: "alice@example.com", EmailVerified: true}

func inGroup(identity oidctest.Identity, groups ...string) oidctest.Identity {
	identity.Groups = groups
	return identity
}

// With administrator groups configured, the provider's groups decide the role
// at every sign-in, and the administrator API refuses to change the role or
// the provider-managed profile.
func TestOIDCRoleFollowsTheProviderWhenAdminGroupsAreConfigured(t *testing.T) {
	a := newOIDCAccounts(t, "/dcm-admins")

	admin := a.signedIn(inGroup(alice, "/dcm-admins"))
	require.Equal(t, model.RoleAdmin, admin.Role)

	listed, err := a.users.ListUsers(accessscope.WithAdminScope(a.ctx, a.root.ID, a.root.Username, model.RoleAdmin), &sso.ListUsersRequest{})
	require.NoError(t, err)
	for _, user := range listed.GetUsers() {
		if user.GetUserId() == admin.ID.String() {
			require.Equal(t, "oidc", user.GetAuthSource())
			require.True(t, user.GetRoleManagedByProvider())
		} else {
			require.Equal(t, "local", user.GetAuthSource())
			require.False(t, user.GetRoleManagedByProvider())
		}
	}

	_, err = a.update(admin, func(r *sso.UpdateUserRequest) { r.Role = model.RoleUser })
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t, "role of this user is managed by the identity provider", status.Convert(err).Message())
	for name, edit := range map[string]func(*sso.UpdateUserRequest){
		"username": func(r *sso.UpdateUserRequest) { r.Username = "alice2" },
		"email":    func(r *sso.UpdateUserRequest) { r.Email = "alice2@example.com" },
		"password": func(r *sso.UpdateUserRequest) { r.Password = "secret123" },
	} {
		_, err = a.update(admin, edit)
		require.Equal(t, codes.InvalidArgument, status.Code(err), name)
		require.Equal(t, "username, email and password of this user are managed by the identity provider", status.Convert(err).Message(), name)
	}

	demoted := a.signedIn(alice)
	require.Equal(t, model.RoleUser, demoted.Role, "leaving the group demotes at the next sign-in")

	deactivated, err := a.update(demoted, func(r *sso.UpdateUserRequest) { r.Status = model.StatusDeactivated })
	require.NoError(t, err, "status changes stay allowed")
	require.Equal(t, model.StatusDeactivated, deactivated.GetStatus())
	require.Equal(t, model.OIDCCallbackAccountDeactivated, a.signIn(alice).Outcome)
}

// With no administrator groups, sign-in never changes the role, so a promotion
// made in DCM survives the next sign-in.
func TestOIDCRoleStaysWithDCMWhenNoAdminGroupsAreConfigured(t *testing.T) {
	a := newOIDCAccounts(t)

	user := a.signedIn(inGroup(alice, "/dcm-admins"))
	require.Equal(t, model.RoleUser, user.Role, "a new OIDC user is a user")

	promoted, err := a.update(user, func(r *sso.UpdateUserRequest) { r.Role = model.RoleAdmin })
	require.NoError(t, err)
	require.False(t, promoted.GetRoleManagedByProvider())

	require.Equal(t, model.RoleAdmin, a.signedIn(alice).Role)
}

// The last active administrator keeps the role and still signs in after
// leaving the administrator group.
func TestOIDCSignInKeepsTheLastActiveAdmin(t *testing.T) {
	a := newOIDCAccounts(t, "/dcm-admins")
	require.Equal(t, model.RoleAdmin, a.signedIn(inGroup(alice, "/dcm-admins")).Role)
	_, err := a.users.DeactivateUser(accessscope.WithAdminScope(a.ctx, a.root.ID, a.root.Username, model.RoleAdmin), &sso.UserIDRequest{UserId: a.root.ID.String()})
	require.NoError(t, err)

	require.Equal(t, model.RoleAdmin, a.signedIn(alice).Role)
}

// Two simultaneous first sign-ins of one new user both succeed as one user.
func TestOIDCSimultaneousFirstSignInsBothSucceed(t *testing.T) {
	a := newOIDCAccounts(t)
	a.idp.SetIdentity(alice)
	type attempt struct{ code, state, binding string }
	attempts := make([]attempt, 2)
	for i := range attempts {
		started, err := a.svc.StartOIDCLogin(a.ctx, "keycloak", "/")
		require.NoError(t, err)
		back := a.idp.SignIn(started.AuthURL)
		attempts[i] = attempt{back.Query().Get("code"), back.Query().Get("state"), started.StateBinding}
	}

	results := make(chan model.OIDCCallbackResult, len(attempts))
	errs := make(chan error, len(attempts))
	for _, at := range attempts {
		go func() {
			result, err := a.svc.CompleteOIDCCallback(a.ctx, "keycloak", at.code, at.state, at.binding)
			errs <- err
			results <- result
		}()
	}
	signedIn := map[uuid.UUID]bool{}
	for range attempts {
		require.NoError(t, <-errs)
		result := <-results
		require.Equal(t, model.OIDCCallbackSuccess, result.Outcome)
		user, err := a.svc.VerifyAccessToken(a.ctx, result.Tokens.AccessToken)
		require.NoError(t, err)
		signedIn[user.ID] = true
	}
	require.Len(t, signedIn, 1, "both sign-ins are the same user")
}

// Two users of one Keycloak user federation with the same name both sign in,
// each with a username of their own, while a local user holds the name.
func TestOIDCFederatedUsersWithOneNameBothSignIn(t *testing.T) {
	a := newOIDCAccounts(t)
	_, err := a.users.CreateUser(accessscope.WithAdminScope(a.ctx, a.root.ID, a.root.Username, model.RoleAdmin), &sso.CreateUserRequest{
		Username: "dave", Email: "dave@example.com", Password: "secret123", Role: model.RoleUser, Status: model.StatusActive,
	})
	require.NoError(t, err)
	const federation = "f:6f1c2b9e-4d3a-4c55-9a7e-0b1d2c3e4f50:"

	first := a.signedIn(oidctest.Identity{Subject: federation + "dave", PreferredUsername: "dave"})
	second := a.signedIn(oidctest.Identity{Subject: federation + "erin", PreferredUsername: "dave"})

	require.Equal(t, "dave-12858549", first.Username)
	require.Equal(t, "dave-d713ee80", second.Username)
}
