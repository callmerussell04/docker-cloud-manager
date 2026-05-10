package dependencywait

import (
	"context"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	cerrdefs "github.com/containerd/errdefs"
)

type ConfigProvider interface {
	Get() config.SystemConfig
}

type Inspector interface {
	InspectContainer(ctx context.Context, dockerID string) (model.ContainerInspection, error)
}

type Options struct {
	MissingIsUnavailable bool
	BeforePoll           func(context.Context) error
}

func Wait(ctx context.Context, cfg ConfigProvider, inspector Inspector, dockerID, condition string, opts Options) error {
	if dockerID == "" && opts.MissingIsUnavailable {
		return missingContainerError()
	}

	snapshot := cfg.Get()
	waitTimeout := time.Duration(snapshot.ComposeDependencyWaitTimeoutMinutes) * time.Minute
	if waitTimeout <= 0 {
		waitTimeout = time.Minute
	}
	pollInterval := time.Duration(snapshot.ComposeDependencyPollIntervalSeconds) * time.Second
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	timeout := time.After(waitTimeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return apperrors.New(apperrors.ErrConflict, "timeout waiting for dependency state")
		case <-ticker.C:
			pollInterval = time.Duration(cfg.Get().ComposeDependencyPollIntervalSeconds) * time.Second
			if pollInterval <= 0 {
				pollInterval = time.Second
			}
			ticker.Reset(pollInterval)
			if opts.BeforePoll != nil {
				if err := opts.BeforePoll(ctx); err != nil {
					return err
				}
			}

			inspect, err := inspector.InspectContainer(ctx, dockerID)
			if err != nil {
				if opts.MissingIsUnavailable && cerrdefs.IsNotFound(err) {
					return missingContainerError()
				}
				continue
			}

			done, err := Evaluate(inspect.State, condition)
			if err != nil || done {
				return err
			}
		}
	}
}

func Evaluate(state model.ContainerState, condition string) (bool, error) {
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
		if !state.Running {
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

func missingContainerError() error {
	return apperrors.New(apperrors.ErrConflict, "container is missing in Docker and can only be deleted")
}
