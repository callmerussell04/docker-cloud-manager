package service

import (
	"context"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/google/uuid"
)

func TestProjectServiceDeleteDelegatesNetworkCleanup(t *testing.T) {
	ownerID := uuid.New()
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
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

	if err := svc.Delete(ctx, projectID); err != nil {
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
	ownerID := uuid.New()
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
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

	if err := svc.Delete(ctx, projectID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if len(containers.cleanedOwnerIDs) != 1 {
		t.Fatalf("network cleanup count = %d, want 1", len(containers.cleanedOwnerIDs))
	}
}

func TestProjectServiceStartFailsWhenRequiredDependencyFails(t *testing.T) {
	ownerID := uuid.New()
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
	projectID := uuid.New()
	dbID := uuid.New()
	webID := uuid.New()

	repo := &projectRepoFake{
		projects: map[uuid.UUID]model.Project{
			projectID: {ID: projectID, OwnerID: ownerID},
		},
		graph: dependencyGraph(projectID, dbID, webID, false),
	}
	dockerAPI := &projectDockerFake{}
	containers := &projectContainerLifecycleFake{}

	svc := NewProjectService(repo, &projectResourceRepoFake{}, dockerAPI, containers, staticConfig{})

	err := svc.Start(ctx, projectID)
	if err == nil {
		t.Fatalf("Start() error = nil, want dependency failure")
	}
	if !strings.Contains(err.Error(), "dependency db failed condition service_healthy") {
		t.Fatalf("Start() error = %q, want required dependency failure", err.Error())
	}
	if containers.started[webID] {
		t.Fatalf("web service was started despite required dependency failure")
	}
}

func TestProjectServiceStartContinuesWhenOptionalDependencyFails(t *testing.T) {
	ownerID := uuid.New()
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
	projectID := uuid.New()
	dbID := uuid.New()
	webID := uuid.New()

	repo := &projectRepoFake{
		projects: map[uuid.UUID]model.Project{
			projectID: {ID: projectID, OwnerID: ownerID},
		},
		graph: dependencyGraph(projectID, dbID, webID, true),
	}
	dockerAPI := &projectDockerFake{}
	containers := &projectContainerLifecycleFake{}

	svc := NewProjectService(repo, &projectResourceRepoFake{}, dockerAPI, containers, staticConfig{})

	if err := svc.Start(ctx, projectID); err != nil {
		t.Fatalf("Start() error = %v, want nil for optional dependency failure", err)
	}
	if !containers.started[webID] {
		t.Fatalf("web service was not started after optional dependency failure")
	}
}

func TestProjectServiceStartTreatsZeroValueDependencyAsRequired(t *testing.T) {
	ownerID := uuid.New()
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
	projectID := uuid.New()
	dbID := uuid.New()
	webID := uuid.New()

	repo := &projectRepoFake{
		projects: map[uuid.UUID]model.Project{
			projectID: {ID: projectID, OwnerID: ownerID},
		},
		graph: []model.ProjectServiceNode{
			{
				ProjectID:   projectID,
				ContainerID: dbID,
				ServiceName: "db",
				StartOrder:  0,
			},
			{
				ProjectID:   projectID,
				ContainerID: webID,
				ServiceName: "web",
				StartOrder:  1,
				Dependencies: []model.ProjectServiceDependency{
					{
						ProjectID:            projectID,
						ContainerID:          webID,
						DependsOnContainerID: dbID,
						DependsOnServiceName: "db",
						Condition:            model.ComposeDependencyConditionHealthy,
					},
				},
			},
		},
	}
	dockerAPI := &projectDockerFake{}
	containers := &projectContainerLifecycleFake{}

	svc := NewProjectService(repo, &projectResourceRepoFake{}, dockerAPI, containers, staticConfig{})

	err := svc.Start(ctx, projectID)
	if err == nil {
		t.Fatalf("Start() error = nil, want zero-value required dependency failure")
	}
	if containers.started[webID] {
		t.Fatalf("web service was started despite zero-value required dependency failure")
	}
}

type projectRepoFake struct {
	projects map[uuid.UUID]model.Project
	graph    []model.ProjectServiceNode
	onDelete func(uuid.UUID)
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

func (f *projectRepoFake) List(ctx context.Context, opts model.ListOptions) ([]model.Project, int, error) {
	var projects []model.Project
	for _, p := range f.projects {
		if opts.OwnerID == nil || p.OwnerID == *opts.OwnerID {
			projects = append(projects, p)
		}
	}
	return projects, len(projects), nil
}

func (f *projectRepoFake) UpdateStatus(ctx context.Context, id uuid.UUID, status string, errorMsg *string) error {
	return nil
}

func (f *projectRepoFake) GetServiceGraph(ctx context.Context, projectID uuid.UUID) ([]model.ProjectServiceNode, error) {
	return f.graph, nil
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
	started         map[uuid.UUID]bool
}

func (f *projectContainerLifecycleFake) Start(ctx context.Context, containerID uuid.UUID) error {
	if f.started == nil {
		f.started = make(map[uuid.UUID]bool)
	}
	f.started[containerID] = true
	return nil
}

func (f *projectContainerLifecycleFake) Stop(ctx context.Context, containerID uuid.UUID) error {
	return nil
}

func (f *projectContainerLifecycleFake) GetByID(ctx context.Context, id uuid.UUID) (model.Container, error) {
	return model.Container{ID: id, DockerID: "docker-id"}, nil
}

func (f *projectContainerLifecycleFake) CleanupUserNetworkIfUnused(ctx context.Context, ownerID uuid.UUID) error {
	f.cleanedOwnerIDs = append(f.cleanedOwnerIDs, ownerID)
	return nil
}

func dependencyGraph(projectID, dbID, webID uuid.UUID, optional bool) []model.ProjectServiceNode {
	return []model.ProjectServiceNode{
		{
			ProjectID:   projectID,
			ContainerID: dbID,
			ServiceName: "db",
			StartOrder:  0,
		},
		{
			ProjectID:   projectID,
			ContainerID: webID,
			ServiceName: "web",
			StartOrder:  1,
			Dependencies: []model.ProjectServiceDependency{
				{
					ProjectID:            projectID,
					ContainerID:          webID,
					DependsOnContainerID: dbID,
					DependsOnServiceName: "db",
					Condition:            model.ComposeDependencyConditionHealthy,
					Optional:             optional,
				},
			},
		},
	}
}
