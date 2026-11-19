package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/google/uuid"
)

// ComposeDeploymentCounter counts an owner's queued and running Compose
// deployments.
type ComposeDeploymentCounter interface {
	CountActiveComposeDeploymentsByOwner(ctx context.Context, ownerID uuid.UUID) (int, error)
}

// ComposePlanContainerRepository holds the owner capacity lock and the
// containers and reservations a Compose plan is counted against.
type ComposePlanContainerRepository interface {
	containerLockRepository
	userReservationReader
	systemReservationReader
	CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error)
}

// ComposePlanVolumeRepository reads the volumes a Compose plan is counted
// against: the owner's by name and in number, the disk they reserve, and the
// host's unused volume promise.
type ComposePlanVolumeRepository interface {
	GetByName(ctx context.Context, ownerID uuid.UUID, name string) (model.Volume, error)
	CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error)
	DiskQuotaVolumeRepository
	volumePromiseReader
}

// ComposePlanBuildRepository counts the builds a Compose plan is counted
// against: the owner's queued builds and every active build on the host.
type ComposePlanBuildRepository interface {
	activeSystemBuildCounter
	CountActiveByOwner(ctx context.Context, ownerID uuid.UUID) (int, error)
}

// ComposePlanAdmission admits a Compose deployment's whole plan at ingestion.
type ComposePlanAdmission struct {
	containers  ComposePlanContainerRepository
	volumes     ComposePlanVolumeRepository
	builds      ComposePlanBuildRepository
	deployments ComposeDeploymentCounter
	ledger      admissionSources
	config      ConfigManager
}

// ComposePlanAdmissionDeps are what a Compose plan is admitted against. Every
// dependency but HostDiskPath and Logger is required.
type ComposePlanAdmissionDeps struct {
	Containers   ComposePlanContainerRepository
	Volumes      ComposePlanVolumeRepository
	Builds       ComposePlanBuildRepository
	Images       DiskQuotaImageRepository
	Deployments  ComposeDeploymentCounter
	Quotas       UserQuotaProvider
	HostMetrics  HostMetricsProvider
	DiskMetrics  HostDiskMetricsProvider
	HostDiskPath string
	Config       ConfigManager
	Logger       *slog.Logger
}

func NewComposePlanAdmission(deps ComposePlanAdmissionDeps) (*ComposePlanAdmission, error) {
	required := []struct {
		name  string
		value any
	}{
		{"containers", deps.Containers}, {"volumes", deps.Volumes},
		{"builds", deps.Builds}, {"images", deps.Images},
		{"deployments", deps.Deployments}, {"quotas", deps.Quotas},
		{"host metrics", deps.HostMetrics}, {"disk metrics", deps.DiskMetrics},
		{"config", deps.Config},
	}
	for _, dependency := range required {
		if dependency.value == nil || isNilServiceDependency(dependency.value) {
			return nil, fmt.Errorf("compose plan admission: %s is required", dependency.name)
		}
	}
	return &ComposePlanAdmission{
		containers: deps.Containers, volumes: deps.Volumes, builds: deps.Builds,
		deployments: deps.Deployments, config: deps.Config,
		ledger: admissionSources{
			quotas:           deps.Quotas,
			userReservations: deps.Containers,
			system:           deps.Containers,
			builds:           deps.Builds,
			images:           deps.Images,
			volumes:          deps.Volumes,
			volumePromise:    deps.Volumes,
			hostMetrics:      deps.HostMetrics,
			diskMetrics:      deps.DiskMetrics,
			hostDiskPath:     deps.HostDiskPath,
			logger:           logging.WithComponent(deps.Logger, "compose_plan_admission"),
		},
	}, nil
}

// AdmitPlan admits demand for the owner in ctx's access scope under the owner
// capacity lock and, when
// it fits, runs commit before the lock is released. It checks, in order, the
// queued Compose deployments, the owner's containers with the plan's
// services, the owner's volumes with the plan's new volumes, the owner's
// queued builds with the plan's builds when it has any, and then the
// admission limits for the plan's memory, CPU and new volume sizes, and
// rejects at the first limit exceeded. A managed volume the owner already has
// by name is reused and asks for nothing; every other one asks for the
// default volume size. Nothing is reserved: each later create, volume create,
// build and start is admitted again. commit and everything called here must
// not take a capacity lock, because the lock pool's single connection is held.
func (a *ComposePlanAdmission) AdmitPlan(ctx context.Context, demand model.ComposePlanDemand, commit func(context.Context) error) error {
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		return err
	}
	cfg := a.config.Get()
	unlock, err := a.containers.AcquireOwnerCapacityLock(ctx, ownerID)
	if err != nil {
		return err
	}
	defer unlock()

	newVolumes, err := a.checkCounts(ctx, ownerID, demand, cfg)
	if err != nil {
		return err
	}
	req := admissionRequest{
		memoryBytes:   demand.MemoryBytes,
		cpuMillicores: demand.CPUMillicores,
		chargesUser:   true,
		volumeBytes:   int64(newVolumes) * max(cfg.DefaultVolumeSizeBytes, 0),
	}
	if err := a.ledger.admit(ctx, ownerID, req, cfg, "compose deployment"); err != nil {
		return err
	}
	return commit(ctx)
}

// checkCounts checks, in order, the owner's queued Compose deployments,
// their containers with the plan's services, their volumes with the plan's
// new volumes, and their queued builds with the plan's builds, and returns
// how many volumes the plan creates.
func (a *ComposePlanAdmission) checkCounts(ctx context.Context, ownerID uuid.UUID, demand model.ComposePlanDemand, cfg config.SystemConfig) (int, error) {
	if cfg.MaxQueuedComposeDeploysPerUser > 0 {
		queued, err := a.deployments.CountActiveComposeDeploymentsByOwner(ctx, ownerID)
		if err != nil {
			return 0, err
		}
		if queued >= cfg.MaxQueuedComposeDeploysPerUser {
			return 0, apperrors.Exceeded(apperrors.LimitQueuedComposeDeployments)
		}
	}
	containers, err := a.containers.CountByOwnerID(ctx, ownerID)
	if err != nil {
		return 0, err
	}
	if containers+demand.Services > cfg.MaxContainersPerUser {
		return 0, apperrors.Exceeded(apperrors.LimitContainersPerUser)
	}
	newVolumes, err := a.newVolumeCount(ctx, ownerID, demand.ManagedVolumeNames)
	if err != nil {
		return 0, err
	}
	if newVolumes > 0 {
		volumes, err := a.volumes.CountByOwnerID(ctx, ownerID)
		if err != nil {
			return 0, err
		}
		if volumes+newVolumes > cfg.MaxVolumesPerUser {
			return 0, apperrors.Exceeded(apperrors.LimitVolumesPerUser)
		}
	}
	if demand.Builds > 0 && cfg.MaxQueuedBuildsPerUser > 0 {
		builds, err := a.builds.CountActiveByOwner(ctx, ownerID)
		if err != nil {
			return 0, err
		}
		if builds+demand.Builds > cfg.MaxQueuedBuildsPerUser {
			return 0, apperrors.Exceeded(apperrors.LimitQueuedBuilds)
		}
	}
	return newVolumes, nil
}

// newVolumeCount is how many of names the owner does not have a volume by.
func (a *ComposePlanAdmission) newVolumeCount(ctx context.Context, ownerID uuid.UUID, names []string) (int, error) {
	count := 0
	for _, name := range names {
		if _, err := a.volumes.GetByName(ctx, ownerID, name); err != nil {
			if !errors.Is(err, apperrors.ErrNotFound) {
				return 0, err
			}
			count++
		}
	}
	return count, nil
}
