package service

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/google/uuid"
)

func TestProjectServiceDeleteDelegatesNetworkCleanup(t *testing.T) {
	ctx := context.Background()
	ownerID := uuid.New()
	projectID := uuid.New()

	repo := &projectRepoFake{
		projects: map[uuid.UUID]model.Project{
			projectID: {ID: projectID, OwnerID: ownerID},
		},
	}
	resources := &projectResourceRepoFake{
		containers: []model.Container{
			{ID: uuid.New(), OwnerID: ownerID, ProjectID: &projectID, DockerID: "docker-project"},
		},
	}
	repo.onDelete = resources.deleteProject
	dockerAPI := &projectDockerFake{}
	containers := &projectContainerLifecycleFake{}

	svc := NewProjectService(repo, resources, dockerAPI, containers, staticConfig{})

	if err := svc.Delete(ctx, ownerID, projectID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if len(containers.cleanedOwnerIDs) != 1 {
		t.Fatalf("network cleanup count = %d, want 1", len(containers.cleanedOwnerIDs))
	}
	if got := containers.cleanedOwnerIDs[0]; got != ownerID {
		t.Fatalf("network cleanup owner = %s, want %s", got, ownerID)
	}
}

func TestProjectServiceDeleteDelegatesNetworkCleanupEvenWhenContainersRemain(t *testing.T) {
	ctx := context.Background()
	ownerID := uuid.New()
	projectID := uuid.New()
	otherProjectID := uuid.New()

	repo := &projectRepoFake{
		projects: map[uuid.UUID]model.Project{
			projectID: {ID: projectID, OwnerID: ownerID},
		},
	}
	resources := &projectResourceRepoFake{
		containers: []model.Container{
			{ID: uuid.New(), OwnerID: ownerID, ProjectID: &projectID, DockerID: "docker-project"},
			{ID: uuid.New(), OwnerID: ownerID, ProjectID: &otherProjectID, DockerID: "docker-other"},
		},
	}
	repo.onDelete = resources.deleteProject
	dockerAPI := &projectDockerFake{}
	containers := &projectContainerLifecycleFake{}

	svc := NewProjectService(repo, resources, dockerAPI, containers, staticConfig{})

	if err := svc.Delete(ctx, ownerID, projectID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if len(containers.cleanedOwnerIDs) != 1 {
		t.Fatalf("network cleanup count = %d, want 1", len(containers.cleanedOwnerIDs))
	}
}

type projectRepoFake struct {
	projects map[uuid.UUID]model.Project
	onDelete func(uuid.UUID)
}

func (f *projectRepoFake) GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]model.Project, error) {
	var projects []model.Project
	for _, p := range f.projects {
		if p.OwnerID == ownerID {
			projects = append(projects, p)
		}
	}
	return projects, nil
}

func (f *projectRepoFake) GetByID(ctx context.Context, id uuid.UUID) (model.Project, error) {
	return f.projects[id], nil
}

func (f *projectRepoFake) Delete(ctx context.Context, id uuid.UUID) error {
	delete(f.projects, id)
	if f.onDelete != nil {
		f.onDelete(id)
	}
	return nil
}

func (f *projectRepoFake) GetAllPaginated(ctx context.Context, limit, offset int) ([]model.Project, int, error) {
	return nil, 0, nil
}

func (f *projectRepoFake) UpdateStatus(ctx context.Context, id uuid.UUID, status string, errorMsg *string) error {
	return nil
}

func (f *projectRepoFake) GetServiceGraph(ctx context.Context, projectID uuid.UUID) ([]model.ProjectServiceNode, error) {
	return nil, nil
}

type projectResourceRepoFake struct {
	containers []model.Container
	volumes    []model.Volume
}

func (f *projectResourceRepoFake) GetByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Container, error) {
	var containers []model.Container
	for _, c := range f.containers {
		if c.ProjectID != nil && *c.ProjectID == projectID {
			containers = append(containers, c)
		}
	}
	return containers, nil
}

func (f *projectResourceRepoFake) GetVolumesByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Volume, error) {
	var volumes []model.Volume
	for _, v := range f.volumes {
		if v.ProjectID != nil && *v.ProjectID == projectID {
			volumes = append(volumes, v)
		}
	}
	return volumes, nil
}

func (f *projectResourceRepoFake) deleteProject(projectID uuid.UUID) {
	filtered := f.containers[:0]
	for _, c := range f.containers {
		if c.ProjectID == nil || *c.ProjectID != projectID {
			filtered = append(filtered, c)
		}
	}
	f.containers = filtered
}

type projectDockerFake struct{}

func (f *projectDockerFake) InspectContainer(ctx context.Context, dockerID string) (model.ContainerInspection, error) {
	return model.ContainerInspection{}, nil
}

func (f *projectDockerFake) StopContainer(ctx context.Context, dockerID string, timeout int) error {
	return nil
}

func (f *projectDockerFake) RemoveContainer(ctx context.Context, dockerID string, force bool) error {
	return nil
}

func (f *projectDockerFake) RemoveVolume(ctx context.Context, volumeName string, force bool) error {
	return nil
}

type projectContainerLifecycleFake struct {
	cleanedOwnerIDs []uuid.UUID
}

func (f *projectContainerLifecycleFake) Start(ctx context.Context, ownerID, containerID uuid.UUID) error {
	return nil
}

func (f *projectContainerLifecycleFake) Stop(ctx context.Context, ownerID, containerID uuid.UUID) error {
	return nil
}

func (f *projectContainerLifecycleFake) GetByID(ctx context.Context, id uuid.UUID) (model.Container, error) {
	return model.Container{ID: id, DockerID: "docker-id"}, nil
}

func (f *projectContainerLifecycleFake) CleanupUserNetworkIfUnused(ctx context.Context, ownerID uuid.UUID) error {
	f.cleanedOwnerIDs = append(f.cleanedOwnerIDs, ownerID)
	return nil
}
