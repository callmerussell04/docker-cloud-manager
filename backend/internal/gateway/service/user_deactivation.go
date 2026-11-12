package service

import (
	"context"
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
)

// DeactivationDirectory deactivates users in SSO.
type DeactivationDirectory interface {
	DeactivateUser(ctx context.Context, userID string) (model.AdminUser, error)
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
// in Core. SSO goes first, so a refused deactivation stops nothing. Repeating
// it is safe: SSO deactivates an already deactivated user again, and Core
// does only the work that remains.
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
