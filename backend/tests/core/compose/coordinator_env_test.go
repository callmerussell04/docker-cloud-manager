package compose_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	compose "github.com/callmerussell04/docker-cloud-manager/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// coordinatorEnv holds the durable store and execution collaborators of one
// coordinator tick. Tests arrange persisted jobs and observe recorded effects.
type coordinatorEnv struct {
	repo       *coordinatorRepo
	builds     *coordinatorBuilds
	volumes    *coordinatorVolumes
	containers *coordinatorContainers
	docker     *coordinatorDocker
	resources  *coordinatorResources
	sources    *coordinatorSources
	images     *coordinatorImages
}

func newCoordinatorEnv(job model.ComposeDeploymentJob) *coordinatorEnv {
	return &coordinatorEnv{
		repo:       newCoordinatorRepo(job),
		builds:     &coordinatorBuilds{},
		volumes:    &coordinatorVolumes{},
		containers: newCoordinatorContainers(nil),
		docker:     &coordinatorDocker{},
		resources:  &coordinatorResources{},
		sources:    &coordinatorSources{},
		images:     &coordinatorImages{},
	}
}

// runOnce runs the coordinator until it has advanced the listed jobs once.
func (e *coordinatorEnv) runOnce(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	e.repo.cancel = cancel
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	orch := compose.NewOrchestrator(ctx, e.repo, e.builds, e.volumes, e.containers, e.docker, coordinatorConfig{}, e.sources, e.builds, e.images, logger)
	orch.SetResourceRepository(e.resources)
	coordinator := compose.NewComposeDeploymentCoordinator(orch, logger)
	done := make(chan struct{})
	go func() {
		defer close(done)
		coordinator.Run(ctx)
	}()
	<-done
}

func runCoordinatorOnce(t *testing.T, repo *coordinatorRepo, containers *coordinatorContainers, docker *coordinatorDocker) {
	t.Helper()
	runCoordinatorOnceWithVolumes(t, repo, nil, containers, docker)
}

func runCoordinatorOnceWithVolumes(t *testing.T, repo *coordinatorRepo, volumes *coordinatorVolumes, containers *coordinatorContainers, docker *coordinatorDocker) {
	t.Helper()
	env := newCoordinatorEnv(repo.job)
	env.repo = repo
	env.containers = containers
	env.docker = docker
	if volumes != nil {
		env.volumes = volumes
	}
	env.runOnce(t)
}

func persistedComposeJob(t *testing.T, ownerID, projectID uuid.UUID, stage string, plan, resources any) model.ComposeDeploymentJob {
	t.Helper()
	planJSON, err := json.Marshal(plan)
	require.NoError(t, err)
	resourceJSON, err := json.Marshal(resources)
	require.NoError(t, err)
	return model.ComposeDeploymentJob{
		ID:              uuid.New(),
		ProjectID:       projectID,
		OwnerID:         ownerID,
		SourceType:      model.ComposeSourceTypeUpload,
		SourceObjectKey: "compose-sources/" + uuid.NewString() + ".zip",
		Status:          model.ComposeDeploymentStatusRunning,
		Stage:           stage,
		RequestID:       "req-" + uuid.NewString(),
		PlanJSON:        planJSON,
		ResourceMapJSON: resourceJSON,
	}
}

type savedProgress struct {
	projectStatus string
	stage         string
	resources     []byte
}

type coordinatorRepo struct {
	job               model.ComposeDeploymentJob
	cancel            context.CancelFunc
	savedResourceMaps [][]byte
	progress          []savedProgress
	projectStatuses   []string
	completedStatus   string
	completedMessage  string
	serviceGraphs     int
}

func newCoordinatorRepo(job model.ComposeDeploymentJob) *coordinatorRepo {
	return &coordinatorRepo{job: job}
}

func (r *coordinatorRepo) lastProgress(t *testing.T) savedProgress {
	t.Helper()
	require.NotEmpty(t, r.progress)
	return r.progress[len(r.progress)-1]
}

func (r *coordinatorRepo) lastResources(t *testing.T, out any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(r.lastProgress(t).resources, out))
}

func (r *coordinatorRepo) ListActiveComposeDeploymentJobs(context.Context, int) ([]model.ComposeDeploymentJob, error) {
	if r.cancel != nil {
		defer r.cancel()
	}
	return []model.ComposeDeploymentJob{r.job}, nil
}
func (r *coordinatorRepo) UpdateComposeDeploymentProgress(_ context.Context, _ uuid.UUID, _ uuid.UUID, projectStatus string, stage string, _ []byte, resourceMapJSON []byte) error {
	saved := append([]byte(nil), resourceMapJSON...)
	r.savedResourceMaps = append(r.savedResourceMaps, saved)
	r.progress = append(r.progress, savedProgress{projectStatus: projectStatus, stage: stage, resources: saved})
	return nil
}
func (r *coordinatorRepo) SaveComposeDeploymentPlan(context.Context, uuid.UUID, string, []byte, []byte) error {
	return nil
}
func (r *coordinatorRepo) CompleteComposeDeploymentJob(_ context.Context, _ uuid.UUID, status string, errorMsg *string) error {
	r.completedStatus = status
	if errorMsg != nil {
		r.completedMessage = *errorMsg
	}
	return nil
}
func (r *coordinatorRepo) UpdateStatus(_ context.Context, _ uuid.UUID, status string, _ *string) error {
	r.projectStatuses = append(r.projectStatuses, status)
	return nil
}
func (r *coordinatorRepo) Save(context.Context, model.Project) error { return nil }
func (r *coordinatorRepo) CreateWithComposeDeploymentJob(context.Context, model.Project, model.ComposeDeploymentJob, model.ComposeDeploymentOutbox) error {
	return nil
}
func (r *coordinatorRepo) GetByID(context.Context, uuid.UUID) (model.Project, error) {
	return model.Project{}, nil
}
func (r *coordinatorRepo) GetComposeDeploymentJob(context.Context, uuid.UUID) (model.ComposeDeploymentJob, error) {
	return r.job, nil
}
func (r *coordinatorRepo) GetActiveComposeDeploymentJobByProjectID(context.Context, uuid.UUID) (model.ComposeDeploymentJob, error) {
	return r.job, nil
}
func (r *coordinatorRepo) ListInterruptedComposeDeploymentJobs(context.Context) ([]model.ComposeDeploymentJob, error) {
	return nil, nil
}
func (r *coordinatorRepo) StartComposeDeploymentJob(context.Context, uuid.UUID) (model.ComposeDeploymentJob, bool, error) {
	return r.job, true, nil
}
func (r *coordinatorRepo) RequestComposeDeploymentCancel(context.Context, uuid.UUID) error {
	return nil
}
func (r *coordinatorRepo) SaveServiceGraph(context.Context, uuid.UUID, []model.ProjectServiceNode) error {
	r.serviceGraphs++
	return nil
}

type coordinatorContainers struct {
	mu               sync.Mutex
	containers       map[uuid.UUID]model.Container
	started          []uuid.UUID
	created          []model.ContainerCreateParams
	createScopes     []accessscope.Scope
	createRequestIDs []string
	nextCreateIDs    []uuid.UUID
	deleted          []uuid.UUID
	deleteErrs       map[uuid.UUID]error
}

func newCoordinatorContainers(items map[uuid.UUID]model.Container) *coordinatorContainers {
	return &coordinatorContainers{containers: items}
}
func (c *coordinatorContainers) GetByID(_ context.Context, id uuid.UUID) (model.Container, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.containers[id], nil
}
func (c *coordinatorContainers) Start(_ context.Context, id uuid.UUID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	item := c.containers[id]
	item.Status = model.ContainerStatusStarting
	c.containers[id] = item
	c.started = append(c.started, id)
	return nil
}
func (c *coordinatorContainers) Create(ctx context.Context, params model.ContainerCreateParams) (uuid.UUID, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := uuid.New()
	if len(c.nextCreateIDs) > 0 {
		id = c.nextCreateIDs[0]
		c.nextCreateIDs = c.nextCreateIDs[1:]
	}
	if c.containers == nil {
		c.containers = make(map[uuid.UUID]model.Container)
	}
	scope, _ := accessscope.FromContext(ctx)
	c.createScopes = append(c.createScopes, scope)
	c.createRequestIDs = append(c.createRequestIDs, logging.RequestIDFromContext(ctx))
	c.created = append(c.created, params)
	c.containers[id] = model.Container{ID: id, Status: model.ContainerStatusCreated}
	return id, nil
}
func (c *coordinatorContainers) Delete(_ context.Context, id uuid.UUID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleted = append(c.deleted, id)
	return c.deleteErrs[id]
}

type coordinatorVolumes struct {
	createIDs       map[string]uuid.UUID
	managedIDs      map[string]uuid.UUID
	resolveIDs      map[string]uuid.UUID
	statuses        map[uuid.UUID]string
	deleteErrs      map[uuid.UUID]error
	created         []model.VolumeCreateParams
	resolved        []string
	managedResolved []string
	deleted         []uuid.UUID
	unlinked        []uuid.UUID
}

func (v *coordinatorVolumes) Create(_ context.Context, params model.VolumeCreateParams) (uuid.UUID, error) {
	v.created = append(v.created, params)
	if id, ok := v.createIDs[params.Name]; ok {
		return id, nil
	}
	return uuid.New(), nil
}

func (v *coordinatorVolumes) ResolveProjectManagedByName(ctx context.Context, projectID uuid.UUID, name string) (uuid.UUID, bool, error) {
	v.managedResolved = append(v.managedResolved, name)
	if id, ok := v.managedIDs[name]; ok {
		return id, true, nil
	}
	id, err := v.Create(ctx, model.VolumeCreateParams{ProjectID: &projectID, Name: name})
	return id, false, err
}

func (v *coordinatorVolumes) ResolveByName(_ context.Context, name string) (uuid.UUID, error) {
	v.resolved = append(v.resolved, name)
	if id, ok := v.resolveIDs[name]; ok {
		return id, nil
	}
	return uuid.Nil, nil
}

// GetByID reports available unless a test arranges another volume status.
func (v *coordinatorVolumes) GetByID(_ context.Context, id uuid.UUID) (model.Volume, error) {
	status, ok := v.statuses[id]
	if !ok {
		status = model.VolumeStatusAvailable
	}
	return model.Volume{ID: id, Status: status}, nil
}

func (v *coordinatorVolumes) Delete(_ context.Context, volumeID uuid.UUID) error {
	v.deleted = append(v.deleted, volumeID)
	return v.deleteErrs[volumeID]
}

func (v *coordinatorVolumes) UnlinkProjectVolume(_ context.Context, _ uuid.UUID, volumeID uuid.UUID) error {
	v.unlinked = append(v.unlinked, volumeID)
	return nil
}

type coordinatorDocker struct {
	inspections map[string]model.ContainerInspection
}

func (d *coordinatorDocker) InspectContainer(_ context.Context, dockerID string) (model.ContainerInspection, error) {
	return d.inspections[dockerID], nil
}

// coordinatorBuilds records build reads, triggers, and cancellations.
type coordinatorBuilds struct {
	builds    map[uuid.UUID]model.Build
	project   []model.Build
	triggered []string
	canceled  []uuid.UUID
}

func (b *coordinatorBuilds) GetByID(_ context.Context, id uuid.UUID) (model.Build, error) {
	return b.builds[id], nil
}
func (b *coordinatorBuilds) GetByProjectID(context.Context, uuid.UUID) ([]model.Build, error) {
	return b.project, nil
}
func (b *coordinatorBuilds) CreateBuildJob(context.Context, string, string, string, string, string, map[string]string, string) (uuid.UUID, uuid.UUID, error) {
	return uuid.Nil, uuid.Nil, errors.New("unexpected standalone build")
}
func (b *coordinatorBuilds) CreateProjectBuildJob(_ context.Context, _ uuid.UUID, serviceName, _, _, _, _, _ string, _ map[string]string, _ string) (uuid.UUID, uuid.UUID, error) {
	b.triggered = append(b.triggered, serviceName)
	return uuid.New(), uuid.New(), nil
}
func (b *coordinatorBuilds) ReserveBuildArchive(context.Context, string) error { return nil }
func (b *coordinatorBuilds) CancelBuildRecord(_ context.Context, id uuid.UUID) error {
	b.canceled = append(b.canceled, id)
	return nil
}

// coordinatorResources lists the containers already persisted for a project.
type coordinatorResources struct {
	containers []model.Container
}

func (r *coordinatorResources) GetByProjectID(context.Context, uuid.UUID) ([]model.Container, error) {
	return r.containers, nil
}
func (r *coordinatorResources) GetVolumesByProjectID(context.Context, uuid.UUID) ([]model.Volume, error) {
	return nil, nil
}

// coordinatorSources records staged Compose source objects the coordinator releases.
type coordinatorSources struct {
	released []string
}

func (s *coordinatorSources) DeleteObject(_ context.Context, key string) error {
	s.released = append(s.released, key)
	return nil
}
func (s *coordinatorSources) UploadStream(context.Context, string, io.Reader, int64, string) error {
	return nil
}
func (s *coordinatorSources) CopyObject(context.Context, string, string, string) error { return nil }
func (s *coordinatorSources) OpenObject(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("unexpected source read")
}
func (s *coordinatorSources) NewReaderAt(context.Context, string) (io.ReaderAt, int64, error) {
	return nil, 0, errors.New("unexpected source read")
}

type coordinatorImages struct {
	deleted []uuid.UUID
}

func (i *coordinatorImages) Delete(_ context.Context, imageID uuid.UUID) error {
	i.deleted = append(i.deleted, imageID)
	return nil
}

type coordinatorConfig struct{}

func (coordinatorConfig) Get() config.SystemConfig {
	return config.SystemConfig{ComposeCoordinatorIntervalSeconds: 1}
}
