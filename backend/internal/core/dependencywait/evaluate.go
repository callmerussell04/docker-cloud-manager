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
// is requested for a container without a healthcheck.
func EvaluateInspection(inspection model.ContainerInspection, condition string) (bool, error) {
	if condition != model.ComposeDependencyConditionHealthy {
		return evaluate(inspection.State, condition)
	}
	if inspection.Healthcheck == nil && inspection.State.HealthStatus == nil {
		return false, apperrors.New(apperrors.ErrBadRequest, "service_healthy requested, but no healthcheck defined for container")
	}
	if inspection.Healthcheck != nil && inspection.State.HealthStatus == nil {
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
		if !state.Running && state.ExitCode != 0 {
			return false, apperrors.New(apperrors.ErrConflict, "dependency exited before becoming healthy")
		}
	case model.ComposeDependencyConditionCompletedSuccessfully:
		if !state.Running && state.Status == "exited" {
			if state.ExitCode == 0 {
				return true, nil
			}
			return false, apperrors.New(apperrors.ErrConflict, "dependency exited with non-zero code")
		}
	case model.ComposeDependencyConditionStarted:
		if state.Running {
			return true, nil
		}
		if !state.Running && state.ExitCode != 0 {
			return false, apperrors.New(apperrors.ErrConflict, "dependency failed to start")
		}
	default:
		return false, apperrors.New(apperrors.ErrBadRequest, "unsupported dependency condition")
	}
	return false, nil
}
