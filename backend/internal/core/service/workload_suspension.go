package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
)

// AccountDeactivatedStopReason is the last error of a container the platform
// stopped because the account that owns it was deactivated.
const AccountDeactivatedStopReason = "stopped by the platform: the account that owns this container was deactivated"

// WorkloadSuspensionContainerRepository lists an owner's containers that
// reserve capacity and queues their platform stops.
type WorkloadSuspensionContainerRepository interface {
	ListReservingByOwner(ctx context.Context, ownerID uuid.UUID) ([]model.Container, error)
	containerStopQueue
}

// WorkloadSuspensionBuildRepository lists an owner's pending and running builds.
type WorkloadSuspensionBuildRepository interface {
	ListActiveByOwner(ctx context.Context, ownerID uuid.UUID) ([]model.Build, error)
}

// WorkloadSuspensionBuildCanceler cancels a build as its owner's cancel does.
type WorkloadSuspensionBuildCanceler interface {
	CancelBuildRecord(ctx context.Context, buildID uuid.UUID) error
}

// WorkloadSuspensionDeploymentRepository lists an owner's queued and running
// Compose deployments; one already canceling is not listed.
type WorkloadSuspensionDeploymentRepository interface {
	ListCancelableComposeDeploymentJobsByOwner(ctx context.Context, ownerID uuid.UUID) ([]model.ComposeDeploymentJob, error)
}

// WorkloadSuspensionDeploymentCanceler cancels a project's Compose deployment
// as its owner's cancel does.
type WorkloadSuspensionDeploymentCanceler interface {
	CancelDeployment(ctx context.Context, projectID uuid.UUID) error
}

type WorkloadSuspensionDeps struct {
	Containers         WorkloadSuspensionContainerRepository
	Builds             WorkloadSuspensionBuildRepository
	BuildCanceler      WorkloadSuspensionBuildCanceler
	Deployments        WorkloadSuspensionDeploymentRepository
	DeploymentCanceler WorkloadSuspensionDeploymentCanceler
	Logger             *slog.Logger
}

// WorkloadSuspension stops a deactivated owner's workloads and keeps their
// data: it queues a platform stop for each container that reserves capacity
// and cancels the owner's active builds and Compose deployments. Records,
// volumes, images and projects stay.
type WorkloadSuspension struct {
	containers         WorkloadSuspensionContainerRepository
	builds             WorkloadSuspensionBuildRepository
	buildCanceler      WorkloadSuspensionBuildCanceler
	deployments        WorkloadSuspensionDeploymentRepository
	deploymentCanceler WorkloadSuspensionDeploymentCanceler
	logger             *slog.Logger
}

func NewWorkloadSuspension(deps WorkloadSuspensionDeps) (*WorkloadSuspension, error) {
	required := []struct {
		name  string
		value any
	}{
		{"containers", deps.Containers}, {"builds", deps.Builds}, {"build canceler", deps.BuildCanceler},
		{"deployments", deps.Deployments}, {"deployment canceler", deps.DeploymentCanceler},
	}
	for _, dependency := range required {
		if dependency.value == nil || isNilServiceDependency(dependency.value) {
			return nil, fmt.Errorf("workload suspension: %s is required", dependency.name)
		}
	}
	return &WorkloadSuspension{
		containers:         deps.Containers,
		builds:             deps.Builds,
		buildCanceler:      deps.BuildCanceler,
		deployments:        deps.Deployments,
		deploymentCanceler: deps.DeploymentCanceler,
		logger:             logging.WithComponent(deps.Logger, "workload_suspension"),
	}, nil
}

// errWorkloadsUnreadable replaces a failure to list an owner's workloads.
var errWorkloadsUnreadable = apperrors.New(apperrors.ErrUnavailable, "the user's workloads could not be read")

// SuspendUserWorkloads needs admin scope. Deployments are canceled first, then
// builds, then containers are stopped, so a deployment's builds and
// containers are handled whichever path reaches them first. A container with
// an active operation is skipped, because a resource has one active operation
// at a time. A workload that fails is logged and counted and the others go
// on. Calling it again does what remains and nothing twice: a stopped
// container no longer reserves capacity, and canceled builds and deployments
// are no longer listed. A list that cannot be read fails the call after the
// other workloads were handled.
func (s *WorkloadSuspension) SuspendUserWorkloads(ctx context.Context, ownerID uuid.UUID) (model.WorkloadSuspension, error) {
	scope, err := accessscope.RequireScope(ctx)
	if err != nil {
		return model.WorkloadSuspension{}, err
	}
	if scope.Kind != accessscope.KindAdmin || scope.UserID == uuid.Nil {
		return model.WorkloadSuspension{}, apperrors.ErrForbidden
	}
	var result model.WorkloadSuspension
	listErr := errors.Join(
		s.cancelDeployments(ctx, ownerID, &result),
		s.cancelBuilds(ctx, ownerID, &result),
		s.stopContainers(ctx, ownerID, &result),
	)
	s.logger.InfoContext(ctx, "user workloads suspended", "user_id", ownerID,
		"stopped_containers", result.StoppedContainers, "skipped_containers", result.SkippedContainers,
		"canceled_builds", result.CanceledBuilds, "canceled_deployments", result.CanceledDeployments, "failed", result.Failed)
	if listErr != nil {
		// %v keeps the cause for the request log without letting its kind
		// decide the response.
		return result, fmt.Errorf("%w: %v", errWorkloadsUnreadable, listErr)
	}
	return result, nil
}

func (s *WorkloadSuspension) cancelDeployments(ctx context.Context, ownerID uuid.UUID, result *model.WorkloadSuspension) error {
	jobs, err := s.deployments.ListCancelableComposeDeploymentJobsByOwner(ctx, ownerID)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to list compose deployments to cancel", "user_id", ownerID, "error", err)
		return fmt.Errorf("list compose deployments: %w", err)
	}
	for _, job := range jobs {
		err := s.deploymentCanceler.CancelDeployment(ctx, job.ProjectID)
		switch {
		case err == nil:
			result.CanceledDeployments++
		case errors.Is(err, apperrors.ErrConflict), errors.Is(err, apperrors.ErrNotFound):
			// The deployment ended or its project went after it was listed.
		default:
			result.Failed++
			s.logger.WarnContext(ctx, "failed to cancel compose deployment of deactivated user", "user_id", ownerID, "project_id", job.ProjectID, "error", err)
		}
	}
	return nil
}

func (s *WorkloadSuspension) cancelBuilds(ctx context.Context, ownerID uuid.UUID, result *model.WorkloadSuspension) error {
	builds, err := s.builds.ListActiveByOwner(ctx, ownerID)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to list builds to cancel", "user_id", ownerID, "error", err)
		return fmt.Errorf("list builds: %w", err)
	}
	for _, build := range builds {
		err := s.buildCanceler.CancelBuildRecord(ctx, build.ID)
		switch {
		case err == nil:
			result.CanceledBuilds++
		case errors.Is(err, apperrors.ErrNotFound):
			// The build was deleted after it was listed.
		default:
			result.Failed++
			s.logger.WarnContext(ctx, "failed to cancel build of deactivated user", "user_id", ownerID, "build_id", build.ID, "error", err)
		}
	}
	return nil
}

func (s *WorkloadSuspension) stopContainers(ctx context.Context, ownerID uuid.UUID, result *model.WorkloadSuspension) error {
	containers, err := s.containers.ListReservingByOwner(ctx, ownerID)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to list containers to stop", "user_id", ownerID, "error", err)
		return fmt.Errorf("list containers: %w", err)
	}
	for _, c := range containers {
		err := queueContainerStop(ctx, s.containers, c, AccountDeactivatedStopReason)
		switch {
		case err == nil:
			result.StoppedContainers++
		case errors.Is(err, apperrors.ErrConflict):
			result.SkippedContainers++
		default:
			result.Failed++
			s.logger.WarnContext(ctx, "failed to queue stop of deactivated user's container", "user_id", ownerID, "container_id", c.ID, "error", err)
		}
	}
	return nil
}
