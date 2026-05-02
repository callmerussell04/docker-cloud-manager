package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
)

func TestContainerServiceExposeFailureKeepsOldContainer(t *testing.T) {
	ownerID := uuid.New()
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
	containerID := uuid.New()
	repo := &containerCleanupRepoFake{
		container: model.Container{
			ID:               containerID,
			OwnerID:          ownerID,
			DockerID:         "old-docker",
			Status:           model.ContainerStatusRunning,
			DockerGeneration: 1,
		},
		containerCount: 1,
	}
	dockerAPI := &containerExposeDockerFake{
		createErr: errors.New("create failed"),
		inspect: model.ContainerInspection{
			Name:              "/usr_old",
			Image:             "nginx:latest",
			MemoryLimitBytes:  128,
			MemoryReservation: 128,
			CPUShares:         1024,
			State:             model.ContainerState{Running: true},
		},
	}
	svc := &ContainerService{
		repo:        repo,
		dockerAPI:   dockerAPI,
		config:      staticConfig{},
		logger:      slog.Default(),
		rebalanceCh: make(chan struct{}, 1),
	}

	if err := svc.Expose(ctx, containerID, "app", 8080); err == nil {
		t.Fatal("Expose() error = nil, want error")
	}

	if dockerAPI.removed["old-docker"] {
		t.Fatal("old docker container was removed on failed expose")
	}
}

func TestContainerServiceDeleteIgnoresMissingDockerContainer(t *testing.T) {
	ownerID := uuid.New()
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
	containerID := uuid.New()
	repo := &containerCleanupRepoFake{
		container:      model.Container{ID: containerID, OwnerID: ownerID, DockerID: "missing-docker"},
		containerCount: 1,
	}
	dockerAPI := &containerExposeDockerFake{removeErr: cerrdefs.ErrNotFound}
	svc := &ContainerService{
		repo:        repo,
		dockerAPI:   dockerAPI,
		config:      staticConfig{},
		logger:      slog.Default(),
		rebalanceCh: make(chan struct{}, 1),
	}

	if err := svc.Delete(ctx, containerID); err != nil {
		t.Fatalf("Delete() error = %v, want nil", err)
	}
	if repo.container.ID != uuid.Nil {
		t.Fatal("container db record was not deleted")
	}
}

func TestContainerServiceStopMarksMissingDockerContainerAndBlocksUse(t *testing.T) {
	ownerID := uuid.New()
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
	containerID := uuid.New()
	repo := &containerCleanupRepoFake{
		container: model.Container{
			ID:       containerID,
			OwnerID:  ownerID,
			DockerID: "missing-docker",
			Status:   model.ContainerStatusRunning,
		},
		containerCount: 1,
	}
	dockerAPI := &containerExposeDockerFake{stopErr: cerrdefs.ErrNotFound}
	svc := &ContainerService{
		repo:        repo,
		dockerAPI:   dockerAPI,
		config:      staticConfig{},
		logger:      slog.Default(),
		rebalanceCh: make(chan struct{}, 1),
	}

	if err := svc.Stop(ctx, containerID); err == nil {
		t.Fatal("Stop() error = nil, want error")
	}
	if repo.container.Status != model.ContainerStatusMissing {
		t.Fatalf("container status = %q, want %q", repo.container.Status, model.ContainerStatusMissing)
	}
}

func TestContainerServiceStartRejectsMissingContainerWithoutDockerCall(t *testing.T) {
	ownerID := uuid.New()
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
	containerID := uuid.New()
	repo := &containerCleanupRepoFake{
		container: model.Container{
			ID:       containerID,
			OwnerID:  ownerID,
			DockerID: "missing-docker",
			Status:   model.ContainerStatusMissing,
		},
		containerCount: 1,
	}
	dockerAPI := &containerExposeDockerFake{}
	svc := &ContainerService{
		repo:        repo,
		dockerAPI:   dockerAPI,
		config:      staticConfig{},
		logger:      slog.Default(),
		rebalanceCh: make(chan struct{}, 1),
	}

	if err := svc.Start(ctx, containerID); err == nil {
		t.Fatal("Start() error = nil, want error")
	}
	if dockerAPI.startCalls != 0 {
		t.Fatalf("StartContainer calls = %d, want 0", dockerAPI.startCalls)
	}
}

func TestContainerServiceRebalancerCoalescesQueuedSignals(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	repo := &rebalancerRepoFake{
		running: []model.Container{{ID: uuid.New(), DockerID: "docker-id", BaseMemoryReservation: 128}},
	}
	dockerAPI := &rebalancerDockerFake{done: make(chan struct{})}
	svc := &ContainerService{
		repo:        repo,
		dockerAPI:   dockerAPI,
		metrics:     staticMetrics{totalMemory: 1024},
		config:      staticConfig{},
		logger:      slog.Default(),
		rebalanceCh: make(chan struct{}, 1),
	}

	for i := 0; i < 100; i++ {
		svc.RequestRebalance()
	}
	go svc.RunRebalancer(ctx)

	select {
	case <-dockerAPI.done:
	case <-time.After(time.Second):
		t.Fatal("rebalancer did not process queued signal")
	}
	cancel()

	if dockerAPI.updates != 1 {
		t.Fatalf("updates = %d, want 1", dockerAPI.updates)
	}
}

type containerExposeDockerFake struct {
	createErr  error
	removeErr  error
	stopErr    error
	inspect    model.ContainerInspection
	removed    map[string]bool
	startCalls int
}

func (f *containerExposeDockerFake) EnsureUserNetwork(ctx context.Context, networkName string) (string, error) {
	return networkName, nil
}

func (f *containerExposeDockerFake) RemoveNetwork(ctx context.Context, networkName string) error {
	return nil
}
func (f *containerExposeDockerFake) PullImage(ctx context.Context, imageName string) error {
	return nil
}

func (f *containerExposeDockerFake) CreateContainer(ctx context.Context, params model.ContainerRuntimeSpec) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	return "new-docker", nil
}

func (f *containerExposeDockerFake) StartContainer(ctx context.Context, dockerID string) error {
	f.startCalls++
	return nil
}
func (f *containerExposeDockerFake) StopContainer(ctx context.Context, dockerID string, timeout int) error {
	return f.stopErr
}

func (f *containerExposeDockerFake) RemoveContainer(ctx context.Context, dockerID string, force bool) error {
	if f.removed == nil {
		f.removed = make(map[string]bool)
	}
	f.removed[dockerID] = true
	return f.removeErr
}

func (f *containerExposeDockerFake) UpdateContainerResources(ctx context.Context, dockerID string, memoryLimit, memoryReservation, cpuShares int64, memorySwapMultiplier float64) error {
	return nil
}

func (f *containerExposeDockerFake) InspectContainer(ctx context.Context, dockerID string) (model.ContainerInspection, error) {
	return f.inspect, nil
}

func (f *containerExposeDockerFake) ImageExists(ctx context.Context, imageTag string) (bool, error) {
	return true, nil
}

func (f *containerExposeDockerFake) GetContainerStats(ctx context.Context, dockerID string) (model.ContainerStats, error) {
	return model.ContainerStats{}, nil
}

type rebalancerRepoFake struct {
	running []model.Container
}

func (f *rebalancerRepoFake) Save(ctx context.Context, c model.Container) error { return nil }
func (f *rebalancerRepoFake) GetByID(ctx context.Context, id uuid.UUID) (model.Container, error) {
	return model.Container{}, nil
}
func (f *rebalancerRepoFake) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	return nil
}
func (f *rebalancerRepoFake) UpdateDockerID(ctx context.Context, id uuid.UUID, dockerID string) error {
	return nil
}
func (f *rebalancerRepoFake) UpdateDockerIDAndStatus(ctx context.Context, id uuid.UUID, dockerID string, status string) error {
	return nil
}
func (f *rebalancerRepoFake) UpdateRouting(ctx context.Context, id uuid.UUID, domainPrefix string, internalPort int) error {
	return nil
}
func (f *rebalancerRepoFake) Delete(ctx context.Context, id uuid.UUID) error { return nil }
func (f *rebalancerRepoFake) GetUserReservedMemory(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	return 0, nil
}
func (f *rebalancerRepoFake) GetRunning(ctx context.Context) ([]model.Container, error) {
	return f.running, nil
}
func (f *rebalancerRepoFake) CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error) {
	return 0, nil
}
func (f *rebalancerRepoFake) GetTotalSystemReservedMemory(ctx context.Context) (int64, error) {
	return 0, nil
}
func (f *rebalancerRepoFake) GetNonExited(ctx context.Context) ([]model.Container, error) {
	return nil, nil
}
func (f *rebalancerRepoFake) CheckDomainPrefixExists(ctx context.Context, prefix string) (bool, error) {
	return false, nil
}
func (f *rebalancerRepoFake) List(ctx context.Context, opts model.ListOptions) ([]model.Container, int, error) {
	return nil, 0, nil
}

type rebalancerDockerFake struct {
	updates int
	done    chan struct{}
}

func (f *rebalancerDockerFake) EnsureUserNetwork(ctx context.Context, networkName string) (string, error) {
	return networkName, nil
}
func (f *rebalancerDockerFake) RemoveNetwork(ctx context.Context, networkName string) error {
	return nil
}
func (f *rebalancerDockerFake) PullImage(ctx context.Context, imageName string) error { return nil }
func (f *rebalancerDockerFake) CreateContainer(ctx context.Context, params model.ContainerRuntimeSpec) (string, error) {
	return "", nil
}
func (f *rebalancerDockerFake) StartContainer(ctx context.Context, dockerID string) error { return nil }
func (f *rebalancerDockerFake) StopContainer(ctx context.Context, dockerID string, timeout int) error {
	return nil
}
func (f *rebalancerDockerFake) RemoveContainer(ctx context.Context, dockerID string, force bool) error {
	return nil
}
func (f *rebalancerDockerFake) UpdateContainerResources(ctx context.Context, dockerID string, memoryLimit, memoryReservation, cpuShares int64, memorySwapMultiplier float64) error {
	f.updates++
	select {
	case <-f.done:
	default:
		close(f.done)
	}
	return nil
}
func (f *rebalancerDockerFake) InspectContainer(ctx context.Context, dockerID string) (model.ContainerInspection, error) {
	return model.ContainerInspection{}, nil
}
func (f *rebalancerDockerFake) ImageExists(ctx context.Context, imageTag string) (bool, error) {
	return true, nil
}
func (f *rebalancerDockerFake) GetContainerStats(ctx context.Context, dockerID string) (model.ContainerStats, error) {
	return model.ContainerStats{}, nil
}

type staticMetrics struct {
	totalMemory int64
}

func (m staticMetrics) GetTotalMemory() (int64, error) { return m.totalMemory, nil }
func (m staticMetrics) GetFreeMemory() (int64, error)  { return m.totalMemory, nil }

type staticConfig struct{}

func (staticConfig) Get() config.SystemConfig {
	return config.SystemConfig{
		ReservedSystemMemory:          0,
		MaxBurstMultiplier:            4,
		DefaultCPUShares:              1024,
		HighLoadCPUShares:             512,
		HighLoadContainerCount:        5,
		ContainerStopTimeout:          1,
		BaseDomain:                    "example.test",
		ContainerPidsLimit:            256,
		ContainerMemorySwapMultiplier: 2,
		ProxyNetworkName:              "proxy_net",
		EventSyncIntervalSeconds:      30,
		EventReconnectDelaySeconds:    5,
		BuildOutboxIntervalSeconds:    1,
		BuildOutboxBatchSize:          10,
	}
}
