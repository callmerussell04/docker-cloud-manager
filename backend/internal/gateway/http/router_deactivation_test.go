package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/permissions"
)

// deactivationCalls records the order in which the fake SSO and Core are
// reached, and what Core audited.
type deactivationCalls struct {
	mu     sync.Mutex
	order  []string
	audits []model.AuditEvent
}

func (c *deactivationCalls) add(call string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.order = append(c.order, call)
}

func (c *deactivationCalls) calls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.order...)
}

// fakeDeactivationSSO deactivates users as SSO does, or fails with err.
type fakeDeactivationSSO struct {
	calls *deactivationCalls
	err   error
}

func (s fakeDeactivationSSO) DeactivateUser(_ context.Context, userID string) (model.AdminUser, error) {
	s.calls.add("sso.DeactivateUser")
	if s.err != nil {
		return model.AdminUser{}, s.err
	}
	return model.AdminUser{UserID: userID, Username: "alice", Email: "alice@example.test", Role: "user", Status: "deactivated", AuthSource: "local"}, nil
}

// UpdateUser saves the requested status as SSO does.
func (s fakeDeactivationSSO) UpdateUser(_ context.Context, userID string, input model.UpdateUserInput) (model.AdminUser, error) {
	s.calls.add("sso.UpdateUser")
	if s.err != nil {
		return model.AdminUser{}, s.err
	}
	return model.AdminUser{UserID: userID, Username: input.Username, Email: input.Email, Role: input.Role, Status: input.Status, AuthSource: "local"}, nil
}

// fakeWorkloadCore suspends workloads as Core does, or fails with err, and
// records audit events.
type fakeWorkloadCore struct {
	calls  *deactivationCalls
	result model.WorkloadSuspension
	err    error
}

func (c fakeWorkloadCore) SuspendUserWorkloads(_ context.Context, _ string) (model.WorkloadSuspension, error) {
	c.calls.add("core.SuspendUserWorkloads")
	return c.result, c.err
}

func (c fakeWorkloadCore) RecordAuditEvent(_ context.Context, event model.AuditEvent) error {
	c.calls.mu.Lock()
	defer c.calls.mu.Unlock()
	c.calls.audits = append(c.calls.audits, event)
	return nil
}

func TestRouterDeactivatesAUserInSSOThenStopsTheirWorkloadsInCore(t *testing.T) {
	user := uuid.NewString()
	tests := []struct {
		name       string
		ssoErr     error
		suspension model.WorkloadSuspension
		coreErr    error
		wantStatus int
		wantCalls  []string
		wantBody   string
		wantError  string
		wantAudit  map[string]string
	}{
		{
			name:       "both succeed",
			suspension: model.WorkloadSuspension{StoppedContainers: 2, CanceledBuilds: 1, CanceledDeployments: 1},
			wantStatus: http.StatusOK,
			wantCalls:  []string{"sso.DeactivateUser", "core.SuspendUserWorkloads"},
			wantBody: `{"user_id":"` + user + `","username":"alice","email":"alice@example.test","role":"user","status":"deactivated","auth_source":"local","role_managed_by_provider":false,
				"workloads":{"stopped_containers":2,"canceled_builds":1,"canceled_deployments":1,"remaining":0}}`,
			wantAudit: map[string]string{"stopped_containers": "2", "canceled_builds": "1", "canceled_deployments": "1", "remaining_workloads": "0"},
		},
		{
			name:       "workloads remain",
			suspension: model.WorkloadSuspension{StoppedContainers: 1, SkippedContainers: 2, Failed: 1},
			wantStatus: http.StatusOK,
			wantCalls:  []string{"sso.DeactivateUser", "core.SuspendUserWorkloads"},
			wantBody: `{"user_id":"` + user + `","username":"alice","email":"alice@example.test","role":"user","status":"deactivated","auth_source":"local","role_managed_by_provider":false,
				"workloads":{"stopped_containers":1,"canceled_builds":0,"canceled_deployments":0,"remaining":3}}`,
			wantAudit: map[string]string{"stopped_containers": "1", "canceled_builds": "0", "canceled_deployments": "0", "remaining_workloads": "3"},
		},
		{
			name:       "core fails",
			coreErr:    errors.New("core: connection refused"),
			wantStatus: http.StatusServiceUnavailable,
			wantCalls:  []string{"sso.DeactivateUser", "core.SuspendUserWorkloads"},
			wantError:  "the account is deactivated, but its workloads could not be stopped; repeat the request to stop them",
		},
		{
			name:       "sso refuses",
			ssoErr:     apperrors.New(apperrors.ErrConflict, "cannot deactivate or demote the last active admin"),
			wantStatus: http.StatusConflict,
			wantCalls:  []string{"sso.DeactivateUser"},
			wantError:  "cannot deactivate or demote the last active admin",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := &deactivationCalls{}
			core := fakeWorkloadCore{calls: calls, result: tc.suspension, err: tc.coreErr}
			h := newSecurityHarnessWithUsers(t, nil, handler.UserManagementDeps{
				Users:        unreachableUserDirectory{},
				AdminUsers:   unreachableAdminUsers{},
				Deactivation: service.NewUserDeactivation(fakeDeactivationSSO{calls: calls, err: tc.ssoErr}, core),
				Audit:        core,
			})
			token := "Bearer admin-" + uuid.NewString()
			h.verifier.grant(token, accessIdentity("admin", func(p string) bool { return p == permissions.UsersAdminDelete }))

			rec := h.serve(http.MethodDelete, "/api/v1/admin/users/"+user, map[string]string{"Authorization": token})

			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			require.Equal(t, tc.wantCalls, calls.calls(), "SSO is called first, and Core only after SSO deactivated the user")
			if tc.wantBody != "" {
				require.JSONEq(t, tc.wantBody, rec.Body.String())
			}
			if tc.wantError != "" {
				var body struct {
					Error string `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				require.Equal(t, tc.wantError, body.Error)
				require.NotContains(t, rec.Body.String(), "connection refused")
			}
			require.Len(t, calls.audits, 1)
			audit := calls.audits[0]
			require.Equal(t, auditlog.ActionUserAdminDeactivate, audit.Action)
			require.Equal(t, user, audit.ResourceID)
			if tc.wantAudit != nil {
				require.Equal(t, auditlog.OutcomeSuccess, audit.Outcome)
				var details map[string]string
				require.NoError(t, json.Unmarshal([]byte(audit.DetailsJSON), &details))
				require.Equal(t, tc.wantAudit, details)
			} else {
				require.Equal(t, auditlog.OutcomeFailure, audit.Outcome)
			}
		})
	}
}

// The edit form can set a user's status, so an update that leaves the account
// deactivated is a deactivation too: it stops the user's workloads and is
// audited as one. Any other update reaches SSO only.
func TestRouterStopsWorkloadsWhenAnUpdateDeactivatesAUser(t *testing.T) {
	user := uuid.NewString()
	account := func(status string) string {
		return `"user_id":"` + user + `","username":"alice","email":"alice@example.test","role":"user","status":"` + status + `","auth_source":"local","role_managed_by_provider":false`
	}
	tests := []struct {
		name       string
		status     string
		ssoErr     error
		suspension model.WorkloadSuspension
		coreErr    error
		wantStatus int
		wantCalls  []string
		wantBody   string
		wantError  string
		wantAudit  string
	}{
		{
			name:       "update leaves the account active",
			status:     "active",
			wantStatus: http.StatusOK,
			wantCalls:  []string{"sso.UpdateUser"},
			wantBody:   `{` + account("active") + `}`,
		},
		{
			name:       "update deactivates the account",
			status:     "deactivated",
			suspension: model.WorkloadSuspension{StoppedContainers: 2, CanceledBuilds: 1, SkippedContainers: 1},
			wantStatus: http.StatusOK,
			wantCalls:  []string{"sso.UpdateUser", "core.SuspendUserWorkloads"},
			wantBody: `{` + account("deactivated") + `,
				"workloads":{"stopped_containers":2,"canceled_builds":1,"canceled_deployments":0,"remaining":1}}`,
			wantAudit: auditlog.OutcomeSuccess,
		},
		{
			name:       "core fails after the account is deactivated",
			status:     "deactivated",
			coreErr:    errors.New("core: connection refused"),
			wantStatus: http.StatusServiceUnavailable,
			wantCalls:  []string{"sso.UpdateUser", "core.SuspendUserWorkloads"},
			wantError:  "the account is deactivated, but its workloads could not be stopped; repeat the request to stop them",
			wantAudit:  auditlog.OutcomeFailure,
		},
		{
			name:       "sso refuses the update",
			status:     "deactivated",
			ssoErr:     apperrors.New(apperrors.ErrConflict, "cannot deactivate or demote the last active admin"),
			wantStatus: http.StatusConflict,
			wantCalls:  []string{"sso.UpdateUser"},
			wantError:  "cannot deactivate or demote the last active admin",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := &deactivationCalls{}
			core := fakeWorkloadCore{calls: calls, result: tc.suspension, err: tc.coreErr}
			h := newSecurityHarnessWithUsers(t, nil, handler.UserManagementDeps{
				Users:        unreachableUserDirectory{},
				AdminUsers:   unreachableAdminUsers{},
				Deactivation: service.NewUserDeactivation(fakeDeactivationSSO{calls: calls, err: tc.ssoErr}, core),
				Audit:        core,
			})
			token := "Bearer admin-" + uuid.NewString()
			h.verifier.grant(token, accessIdentity("admin", func(p string) bool { return p == permissions.UsersAdminUpdate }))

			rec := h.serveBody(http.MethodPut, "/api/v1/admin/users/"+user,
				`{"username":"alice","email":"alice@example.test","role":"user","status":"`+tc.status+`"}`,
				map[string]string{"Authorization": token, "Content-Type": "application/json"})

			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			require.Equal(t, tc.wantCalls, calls.calls(), "SSO is called first, and Core only for an account SSO left deactivated")
			if tc.wantBody != "" {
				require.JSONEq(t, tc.wantBody, rec.Body.String())
			}
			if tc.wantError != "" {
				var body struct {
					Error string `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				require.Equal(t, tc.wantError, body.Error)
				require.NotContains(t, rec.Body.String(), "connection refused")
			}
			if tc.wantAudit == "" {
				require.Empty(t, calls.audits, "only a deactivation is audited as one")
				return
			}
			require.Len(t, calls.audits, 1)
			require.Equal(t, auditlog.ActionUserAdminDeactivate, calls.audits[0].Action)
			require.Equal(t, user, calls.audits[0].ResourceID)
			require.Equal(t, tc.wantAudit, calls.audits[0].Outcome)
		})
	}
}

// fakeReactivationSSO reactivates users as SSO does.
type fakeReactivationSSO struct {
	unreachableUserDirectory
	calls *deactivationCalls
}

func (s fakeReactivationSSO) ReactivateUser(_ context.Context, userID string) (model.AdminUser, error) {
	s.calls.add("sso.ReactivateUser")
	return model.AdminUser{UserID: userID, Username: "alice", Role: "user", Status: "active"}, nil
}

// Reactivation changes the account only: it starts nothing in Core.
func TestRouterReactivatesAUserWithoutTouchingTheirWorkloads(t *testing.T) {
	calls := &deactivationCalls{}
	core := fakeWorkloadCore{calls: calls}
	h := newSecurityHarnessWithUsers(t, nil, handler.UserManagementDeps{
		Users:        fakeReactivationSSO{calls: calls},
		AdminUsers:   unreachableAdminUsers{},
		Deactivation: service.NewUserDeactivation(fakeDeactivationSSO{calls: calls}, core),
		Audit:        core,
	})
	token := "Bearer admin-" + uuid.NewString()
	h.verifier.grant(token, accessIdentity("admin", func(p string) bool { return p == permissions.UsersAdminUpdate }))

	rec := h.serve(http.MethodPost, "/api/v1/admin/users/"+uuid.NewString()+"/activate", map[string]string{"Authorization": token})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, []string{"sso.ReactivateUser"}, calls.calls())
}
