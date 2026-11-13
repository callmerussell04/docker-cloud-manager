package service

import (
	"context"
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
)

// userStatusDeactivated is SSO's status of a deactivated account.
const userStatusDeactivated = "deactivated"

// DeactivationDirectory changes users in SSO in the ways that can deactivate
// them.
type DeactivationDirectory interface {
	DeactivateUser(ctx context.Context, userID string) (model.AdminUser, error)
	UpdateUser(ctx context.Context, userID string, input model.UpdateUserInput) (model.AdminUser, error)
}

// WorkloadSuspender stops a user's workloads in Core.
type WorkloadSuspender interface {
	SuspendUserWorkloads(ctx context.Context, userID string) (model.WorkloadSuspension, error)
}

// errWorkloadsNotStopped replaces every failure to suspend the workloads of a
// user SSO already deactivated.
var errWorkloadsNotStopped = apperrors.New(apperrors.ErrUnavailable,
	"the account is deactivated, but its workloads could not be stopped; repeat the request to stop them")

// UserDeactivation deactivates a user in SSO and then stops their workloads
// in Core, whichever way the account is deactivated: by the deactivation
// itself or by an update that sets its status. SSO goes first, so a refused
// change stops nothing. Repeating either is safe: SSO saves the same status
// again, and Core does only the work that remains.
type UserDeactivation struct {
	users     DeactivationDirectory
	workloads WorkloadSuspender
}

func NewUserDeactivation(users DeactivationDirectory, workloads WorkloadSuspender) *UserDeactivation {
	return &UserDeactivation{users: users, workloads: workloads}
}

func (s *UserDeactivation) DeactivateUser(ctx context.Context, userID string) (model.UserDeactivation, error) {
	user, err := s.users.DeactivateUser(ctx, userID)
	if err != nil {
		return model.UserDeactivation{}, err
	}
	workloads, err := s.workloads.SuspendUserWorkloads(ctx, userID)
	if err != nil {
		// %v keeps the cause for the request log without letting its kind
		// decide the response.
		return model.UserDeactivation{}, fmt.Errorf("%w: suspend workloads: %v", errWorkloadsNotStopped, err)
	}
	return model.UserDeactivation{User: user, Workloads: workloads}, nil
}

// UpdateUser saves the account in SSO. An update that leaves the account
// deactivated is a deactivation, so the user's workloads are stopped as
// DeactivateUser stops them; any other update touches nothing in Core.
func (s *UserDeactivation) UpdateUser(ctx context.Context, userID string, input model.UpdateUserInput) (model.UserUpdate, error) {
	user, err := s.users.UpdateUser(ctx, userID, input)
	if err != nil {
		return model.UserUpdate{}, err
	}
	if user.Status != userStatusDeactivated {
		return model.UserUpdate{User: user}, nil
	}
	workloads, err := s.workloads.SuspendUserWorkloads(ctx, userID)
	if err != nil {
		// %v keeps the cause for the request log without letting its kind
		// decide the response.
		return model.UserUpdate{User: user, Deactivated: true}, fmt.Errorf("%w: suspend workloads: %v", errWorkloadsNotStopped, err)
	}
	return model.UserUpdate{User: user, Deactivated: true, Workloads: workloads}, nil
}
