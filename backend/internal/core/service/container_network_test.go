package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/google/uuid"
)

func TestContainerServiceCleanupUserNetworkIfUnusedRemovesNetwork(t *testing.T) {
	ctx := context.Background()
	ownerID := uuid.New()
	repo := &containerCleanupRepoFake{containerCount: 0}
	dockerAPI := &containerCleanupDockerFake{}
	svc := &ContainerService{repo: repo, dockerAPI: dockerAPI, logger: slog.Default()}

	if err := svc.CleanupUserNetworkIfUnused(ctx, ownerID); err != nil {
		t.Fatalf("CleanupUserNetworkIfUnused() error = %v", err)
	}

	if len(dockerAPI.removedNetworks) != 1 {
		t.Fatalf("removed networks count = %d, want 1", len(dockerAPI.removedNetworks))
	}
	if got, want := dockerAPI.removedNetworks[0], userNetworkName(ownerID); got != want {
		t.Fatalf("removed network = %q, want %q", got, want)
	}
}

func TestContainerServiceCleanupUserNetworkIfUnusedKeepsNetwork(t *testing.T) {
	ctx := context.Background()
	ownerID := uuid.New()
	repo := &containerCleanupRepoFake{containerCount: 1}
	dockerAPI := &containerCleanupDockerFake{}
	svc := &ContainerService{repo: repo, dockerAPI: dockerAPI, logger: slog.Default()}

	if err := svc.CleanupUserNetworkIfUnused(ctx, ownerID); err != nil {
		t.Fatalf("CleanupUserNetworkIfUnused() error = %v", err)
	}

	if len(dockerAPI.removedNetworks) != 0 {
		t.Fatalf("removed networks count = %d, want 0", len(dockerAPI.removedNetworks))
	}
}

func TestContainerServiceAdminDeleteCleansUserNetwork(t *testing.T) {
	ctx := context.Background()
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := &containerCleanupRepoFake{
		container: model.Container{ID: containerID, OwnerID: ownerID, DockerID: "docker-id"},
	}
	dockerAPI := &containerCleanupDockerFake{}
	svc := &ContainerService{repo: repo, dockerAPI: dockerAPI, logger: slog.Default()}

	if err := svc.AdminDelete(ctx, containerID); err != nil {
		t.Fatalf("AdminDelete() error = %v", err)
	}

	if len(dockerAPI.removedNetworks) != 1 {
		t.Fatalf("removed networks count = %d, want 1", len(dockerAPI.removedNetworks))
	}
}

type containerCleanupRepoFake struct {
	container      model.Container
	containerCount int
}

func (f *containerCleanupRepoFake) Save(ctx context.Context, c model.Container) error {
	f.container = c
	f.containerCount++
	return nil
}

func (f *containerCleanupRepoFake) GetByID(ctx context.Context, id uuid.UUID) (model.Container, error) {
	if f.container.ID == id {
		return f.container, nil
	}
	return model.Container{}, errors.New("not found")
}

func (f *containerCleanupRepoFake) GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]model.Container, error) {
	if f.container.OwnerID == ownerID {
		return []model.Container{f.container}, nil
	}
	return nil, nil
}

func (f *containerCleanupRepoFake) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	return nil
}

func (f *containerCleanupRepoFake) UpdateDockerID(ctx context.Context, id uuid.UUID, dockerID string) error {
	return nil
}

func (f *containerCleanupRepoFake) UpdateDockerIDAndStatus(ctx context.Context, id uuid.UUID, dockerID string, status string) error {
	return nil
}

func (f *containerCleanupRepoFake) UpdateRouting(ctx context.Context, id uuid.UUID, domainPrefix string, internalPort int) error {
	return nil
}

func (f *containerCleanupRepoFake) Delete(ctx context.Context, id uuid.UUID) error {
	f.container = model.Container{}
	f.containerCount = 0
	return nil
}

func (f *containerCleanupRepoFake) GetUserReservedMemory(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	return 0, nil
}

func (f *containerCleanupRepoFake) GetRunning(ctx context.Context) ([]model.Container, error) {
	return nil, nil
}

func (f *containerCleanupRepoFake) CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error) {
	return f.containerCount, nil
}

func (f *containerCleanupRepoFake) GetTotalSystemReservedMemory(ctx context.Context) (int64, error) {
	return 0, nil
}

func (f *containerCleanupRepoFake) GetNonExited(ctx context.Context) ([]model.Container, error) {
	return nil, nil
}

func (f *containerCleanupRepoFake) CheckDomainPrefixExists(ctx context.Context, prefix string) (bool, error) {
	return false, nil
}

func (f *containerCleanupRepoFake) GetAllPaginated(ctx context.Context, limit, offset int) ([]model.Container, int, error) {
	return nil, 0, nil
}

type containerCleanupDockerFake struct {
	removedNetworks []string
}

func (f *containerCleanupDockerFake) EnsureUserNetwork(ctx context.Context, networkName string) (string, error) {
	return networkName, nil
}

func (f *containerCleanupDockerFake) RemoveNetwork(ctx context.Context, networkName string) error {
	f.removedNetworks = append(f.removedNetworks, networkName)
	return nil
}

func (f *containerCleanupDockerFake) PullImage(ctx context.Context, imageName string) error {
	return nil
}

func (f *containerCleanupDockerFake) CreateContainer(ctx context.Context, params model.ContainerRuntimeSpec) (string, error) {
	return "docker-id", nil
}

func (f *containerCleanupDockerFake) StartContainer(ctx context.Context, dockerID string) error {
	return nil
}

func (f *containerCleanupDockerFake) StopContainer(ctx context.Context, dockerID string, timeout int) error {
	return nil
}

func (f *containerCleanupDockerFake) RemoveContainer(ctx context.Context, dockerID string, force bool) error {
	return nil
}

func (f *containerCleanupDockerFake) UpdateContainerResources(ctx context.Context, dockerID string, memoryLimit, memoryReservation, cpuShares int64) error {
	return nil
}

func (f *containerCleanupDockerFake) InspectContainer(ctx context.Context, dockerID string) (model.ContainerInspection, error) {
	return model.ContainerInspection{}, nil
}

func (f *containerCleanupDockerFake) ImageExists(ctx context.Context, imageTag string) (bool, error) {
	return true, nil
}

func (f *containerCleanupDockerFake) GetContainerStats(ctx context.Context, dockerID string) (model.ContainerStats, error) {
	return model.ContainerStats{}, nil
}
