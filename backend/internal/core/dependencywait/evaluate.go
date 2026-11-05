// Package dependencywait decides whether a Compose service dependency has
// reached the state its depends_on condition requires.
package dependencywait

import (
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
)

// EvaluateInspection reports whether an inspected dependency container meets
// condition. It returns false with no error while the dependency may still
// get there, and an error once it cannot: ErrConflict when the dependency
// failed, ErrBadRequest when the condition is unsupported or service_healthy
// is requested for a container without a healthcheck. A stopped (exited or
// dead) container never changes state on its own, so it is never waited on.
func EvaluateInspection(inspection model.ContainerInspection, condition string) (bool, error) {
	if condition != model.ComposeDependencyConditionHealthy {
		return evaluate(inspection.State, condition)
	}
	if inspection.Healthcheck == nil && inspection.State.HealthStatus == nil {
		return false, apperrors.New(apperrors.ErrBadRequest, "service_healthy requested, but no healthcheck defined for container")
	}
	if inspection.Healthcheck != nil && inspection.State.HealthStatus == nil {
		if stopped(inspection.State) {
			return false, apperrors.New(apperrors.ErrConflict, "dependency exited before becoming healthy")
		}
		return false, nil
	}
	return evaluate(inspection.State, condition)
}

func evaluate(state model.ContainerState, condition string) (bool, error) {
	switch condition {
	case model.ComposeDependencyConditionHealthy:
		if state.HealthStatus == nil {
			return false, apperrors.New(apperrors.ErrBadRequest, "service_healthy requested, but no healthcheck defined for container")
		}
		if *state.HealthStatus == "healthy" {
			return true, nil
		}
		if *state.HealthStatus == "unhealthy" {
			return false, apperrors.New(apperrors.ErrConflict, "dependency became unhealthy")
		}
		if stopped(state) {
			return false, apperrors.New(apperrors.ErrConflict, "dependency exited before becoming healthy")
		}
	case model.ComposeDependencyConditionCompletedSuccessfully:
		if state.Running {
			return false, nil
		}
		if state.Status == containerStatusDead {
			return false, apperrors.New(apperrors.ErrConflict, "dependency died before completing")
		}
		if state.Status == containerStatusExited {
			if state.ExitCode == 0 {
				return true, nil
			}
			return false, apperrors.New(apperrors.ErrConflict, "dependency exited with non-zero code")
		}
	case model.ComposeDependencyConditionStarted:
		if state.Running {
			return true, nil
		}
		if state.Status == containerStatusExited && state.ExitCode == 0 {
			// A quick one-shot dependency may start and exit between checks.
			return true, nil
		}
		if state.Status == containerStatusDead || state.ExitCode != 0 {
			return false, apperrors.New(apperrors.ErrConflict, "dependency failed to start")
		}
	default:
		return false, apperrors.New(apperrors.ErrBadRequest, "unsupported dependency condition")
	}
	return false, nil
}

const (
	containerStatusExited = "exited"
	containerStatusDead   = "dead"
)

// stopped reports whether Docker has stopped the container for good: it
// exited or died and is not running.
func stopped(state model.ContainerState) bool {
	return !state.Running && (state.Status == containerStatusExited || state.Status == containerStatusDead)
}
