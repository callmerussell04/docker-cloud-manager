package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
)

type EventContainerRepo interface {
	UpdateStatusByDockerID(ctx context.Context, dockerID string, status string) error
	GetByDockerID(ctx context.Context, dockerID string) (model.Container, error)
	GetNonExited(ctx context.Context) ([]model.Container, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error
}

type EventDockerAPI interface {
	ListenEvents(ctx context.Context) (<-chan model.ContainerEvent, <-chan error)
	InspectContainer(ctx context.Context, dockerID string) (model.ContainerInspection, error)
	InspectVolume(ctx context.Context, volumeName string) (model.VolumeInspection, error)
}

type EventVolumeRepo interface {
	GetReconcileCandidates(ctx context.Context) ([]model.Volume, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error
}

type ContainerRebalancer interface {
	RequestRebalance()
}

type EventProjectStatusUpdater interface {
	RefreshProjectStatus(ctx context.Context, projectID uuid.UUID) error
}

type EventConfigProvider interface {
	Get() config.SystemConfig
}

type EventWorker struct {
	repo       EventContainerRepo
	volumeRepo EventVolumeRepo
	dockerAPI  EventDockerAPI
	rebalancer ContainerRebalancer
	projects   EventProjectStatusUpdater
	cfg        EventConfigProvider
	logger     *slog.Logger
}

func NewEventWorker(repo EventContainerRepo, volumeRepo EventVolumeRepo, dockerAPI EventDockerAPI, rebalancer ContainerRebalancer, projects EventProjectStatusUpdater, cfg EventConfigProvider, logger *slog.Logger) *EventWorker {
	return &EventWorker{
		repo:       repo,
		volumeRepo: volumeRepo,
		dockerAPI:  dockerAPI,
		rebalancer: rebalancer,
		projects:   projects,
		cfg:        cfg,
		logger:     logging.WithComponent(logger, "event_worker"),
	}
}

func (w *EventWorker) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "event worker started")
	w.syncState(ctx)

	syncTicker := time.NewTicker(time.Duration(w.cfg.Get().EventSyncIntervalSeconds) * time.Second)
	defer syncTicker.Stop()

	var msgCh <-chan model.ContainerEvent
	var errCh <-chan error
	reconnectAt := time.NewTimer(0)
	defer reconnectAt.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.InfoContext(ctx, "event worker stopped")
			return
		case <-syncTicker.C:
			w.syncState(ctx)
			resetTicker(syncTicker, time.Duration(w.cfg.Get().EventSyncIntervalSeconds)*time.Second)
		case <-reconnectAt.C:
			msgCh, errCh = w.dockerAPI.ListenEvents(ctx)
		case err, ok := <-errCh:
			if !ok {
				msgCh, errCh = nil, nil
				resetTimer(reconnectAt, time.Duration(w.cfg.Get().EventReconnectDelaySeconds)*time.Second)
				continue
			}
			if err != nil {
				w.logger.ErrorContext(ctx, "docker event stream error", "error", err)
				msgCh, errCh = nil, nil
				resetTimer(reconnectAt, time.Duration(w.cfg.Get().EventReconnectDelaySeconds)*time.Second)
			}
		case msg, ok := <-msgCh:
			if !ok {
				msgCh, errCh = nil, nil
				resetTimer(reconnectAt, time.Duration(w.cfg.Get().EventReconnectDelaySeconds)*time.Second)
				continue
			}
			if msg.Type == "container" {
				switch msg.Action {
				case "start":
					_ = w.repo.UpdateStatusByDockerID(ctx, msg.DockerID, model.ContainerStatusRunning)
					w.refreshProjectStatusByDockerID(ctx, msg.DockerID)
					w.rebalancer.RequestRebalance()
				case "die", "stop", "kill", "oom":
					_ = w.repo.UpdateStatusByDockerID(ctx, msg.DockerID, model.ContainerStatusExited)
					w.refreshProjectStatusByDockerID(ctx, msg.DockerID)
					w.rebalancer.RequestRebalance()
				case "destroy":
					_ = w.repo.UpdateStatusByDockerID(ctx, msg.DockerID, model.ContainerStatusMissing)
					w.refreshProjectStatusByDockerID(ctx, msg.DockerID)
					w.rebalancer.RequestRebalance()
				}
			}
		}
	}
}

func (w *EventWorker) syncState(ctx context.Context) {
	w.syncContainers(ctx)
	w.syncVolumes(ctx)
}

func (w *EventWorker) syncContainers(ctx context.Context) {
	containers, err := w.repo.GetNonExited(ctx)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to fetch non-exited containers", "error", err)
		return
	}

	changed := false

	for _, c := range containers {
		if c.DockerID == "" {
			_ = w.repo.MarkStatusError(ctx, c.ID, model.ContainerStatusMissing, resourceMissingError("container"))
			w.logger.WarnContext(ctx, "container has empty docker id", "container_id", c.ID)
			continue
		}

		inspect, err := w.dockerAPI.InspectContainer(ctx, c.DockerID)
		if err != nil {
			if cerrdefs.IsNotFound(err) {
				_ = w.repo.MarkStatusError(ctx, c.ID, model.ContainerStatusMissing, resourceMissingError("container"))
				w.logger.WarnContext(ctx, "docker container missing during state sync", "container_id", c.ID, "docker_id", c.DockerID)
				w.refreshProjectStatus(ctx, c.ProjectID)
				changed = true
			}
			continue
		}

		expectedStatus := model.ContainerStatusExited
		if inspect.State.Running {
			expectedStatus = model.ContainerStatusRunning
		} else if c.Status == model.ContainerStatusCreated && inspect.State.Status == "created" {
			expectedStatus = model.ContainerStatusCreated
		}

		if c.Status != expectedStatus {
			_ = w.repo.UpdateStatus(ctx, c.ID, expectedStatus)
			w.logger.InfoContext(ctx, "container status synced", "container_id", c.ID, "old_status", c.Status, "new_status", expectedStatus)
			w.refreshProjectStatus(ctx, c.ProjectID)
			if expectedStatus == model.ContainerStatusRunning || c.Status == model.ContainerStatusRunning {
				changed = true
			}
		}
	}

	if changed {
		w.rebalancer.RequestRebalance()
	}
}

func (w *EventWorker) refreshProjectStatusByDockerID(ctx context.Context, dockerID string) {
	if w.projects == nil {
		return
	}
	c, err := w.repo.GetByDockerID(ctx, dockerID)
	if err != nil {
		return
	}
	w.refreshProjectStatus(ctx, c.ProjectID)
}

func (w *EventWorker) refreshProjectStatus(ctx context.Context, projectID *uuid.UUID) {
	if w.projects == nil || projectID == nil {
		return
	}
	if err := w.projects.RefreshProjectStatus(ctx, *projectID); err != nil {
		w.logger.WarnContext(ctx, "failed to refresh project status", "project_id", *projectID, "error", err)
	}
}

func (w *EventWorker) syncVolumes(ctx context.Context) {
	if w.volumeRepo == nil {
		return
	}

	volumes, err := w.volumeRepo.GetReconcileCandidates(ctx)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to fetch volume reconcile candidates", "error", err)
		return
	}

	for _, v := range volumes {
		if v.DockerName == "" {
			_ = w.volumeRepo.MarkStatusError(ctx, v.ID, model.VolumeStatusMissing, resourceMissingError("volume"))
			w.logger.WarnContext(ctx, "volume has empty docker name", "volume_id", v.ID)
			continue
		}

		if _, err := w.dockerAPI.InspectVolume(ctx, v.DockerName); err != nil {
			if cerrdefs.IsNotFound(err) {
				_ = w.volumeRepo.MarkStatusError(ctx, v.ID, model.VolumeStatusMissing, resourceMissingError("volume"))
				w.logger.WarnContext(ctx, "docker volume missing during state sync", "volume_id", v.ID, "docker_name", v.DockerName)
			}
			continue
		}

		if v.Status == model.VolumeStatusCreating {
			_ = w.volumeRepo.UpdateStatus(ctx, v.ID, model.VolumeStatusAvailable)
		}
	}
}

func resourceMissingError(resource string) error {
	if resource == "" {
		resource = "resource"
	}
	return errors.New(resource + " is missing in Docker; delete it")
}

func resetTimer(timer *time.Timer, delay time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(delay)
}

func resetTicker(ticker *time.Ticker, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker.Reset(interval)
}
