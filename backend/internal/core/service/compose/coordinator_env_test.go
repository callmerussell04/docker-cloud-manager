package compose_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// coordinatorEnv holds the durable store and execution collaborators of one
// coordinator tick. Tests arrange persisted jobs and observe recorded effects.
// Stateful recording fakes replace mockery here: a tick re-reads state it just
// changed (containers created then inspected) and iterates resource maps in
// random order, which per-call expectations cannot express without coupling
// tests to call sequence.
type coordinatorEnv struct {
	repo       *deploymentRepo
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
		repo:       newDeploymentRepo(job),
		builds:     &coordinatorBuilds{},
		volumes:    &coordinatorVolumes{},
		containers: newCoordinatorContainers(nil),
		docker:     &coordinatorDocker{},
		resources:  &coordinatorResources{},
		sources:    &coordinatorSources{},
		images:     &coordinatorImages{},
	}
}

func (e *coordinatorEnv) deps() compose.DeploymentCoordinatorDeps {
	return compose.DeploymentCoordinatorDeps{
		Deployments:       e.repo,
		Builds:            e.builds,
		Builder:           e.builds,
		Containers:        e.containers,
		ProjectContainers: e.resources,
		Volumes:           e.volumes,
		Docker:            e.docker,
		Images:            e.images,
		Sources:           e.sources,
		Config:            staticConfig{cfg: config.SystemConfig{ComposeCoordinatorIntervalSeconds: 1}},
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// runOnce runs the coordinator until it has advanced the listed jobs once.
func (e *coordinatorEnv) runOnce(t *testing.T) {
	t.Helper()
	coordinator, err := compose.NewDeploymentCoordinator(e.deps())
	require.NoError(t, err)
	runCoordinatorTick(coordinator, e.repo)
}

// runCoordinatorTick runs coordinator for one pass: listing the active jobs
// stops the run once they are advanced.
func runCoordinatorTick(coordinator *compose.DeploymentCoordinator, repo *deploymentRepo) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo.cancel = cancel
	done := make(chan struct{})
	go func() {
		defer close(done)
		coordinator.Run(ctx)
	}()
	<-done
	repo.cancel = nil
}

func runCoordinatorOnce(t *testing.T, repo *deploymentRepo, containers *coordinatorContainers, docker *coordinatorDocker) {
	t.Helper()
	runCoordinatorOnceWithVolumes(t, repo, nil, containers, docker)
}

func runCoordinatorOnceWithVolumes(t *testing.T, repo *deploymentRepo, volumes *coordinatorVolumes, containers *coordinatorContainers, docker *coordinatorDocker) {
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

// deploymentRepoFaults fails the next calls of a store method: each queued
// error fails one call, then the method succeeds again.
type deploymentRepoFaults struct {
	GetProject  []error
	SavePlan    []error
	RequeueJob  []error
	CompleteJob []error
}

// deploymentRepo is the durable state of one Compose deployment: its project,
// its job and the job's outbox record. It serves both the message handler
// (compose.ProjectRepository) and the coordinator (compose.DeploymentStore)
// and mirrors the status guards of the Postgres repository. It also records
// the progress and status writes that stage tests assert on.
type deploymentRepo struct {
	job               model.ComposeDeploymentJob
	project           model.Project
	outbox            []model.ComposeDeploymentOutbox
	serviceGraph      []model.ProjectServiceNode
	faults            deploymentRepoFaults
	cancel            context.CancelFunc
	savedResourceMaps [][]byte
	progress          []savedProgress
	projectStatuses   []string
	completedStatus   string
	completedMessage  string
	serviceGraphs     int
}

func newDeploymentRepo(job model.ComposeDeploymentJob) *deploymentRepo {
	return &deploymentRepo{
		job:     job,
		project: model.Project{ID: job.ProjectID, OwnerID: job.OwnerID, Name: "wh", Status: model.ProjectStatusDeploying},
	}
}

func (r *deploymentRepo) lastProgress(t *testing.T) savedProgress {
	t.Helper()
	require.NotEmpty(t, r.progress)
	return r.progress[len(r.progress)-1]
}

func (r *deploymentRepo) lastResources(t *testing.T, out any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(r.lastProgress(t).resources, out))
}

func jobStatusIn(job model.ComposeDeploymentJob, statuses ...string) bool {
	return slices.Contains(statuses, job.Status)
}

// jobActive reports whether a job is queued, running or canceling.
func jobActive(job model.ComposeDeploymentJob) bool {
	return jobStatusIn(job, model.ComposeDeploymentStatusQueued, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStatusCanceling)
}

func (r *deploymentRepo) Save(_ context.Context, p model.Project) error {
	r.project = p
	return nil
}

func (r *deploymentRepo) CreateWithComposeDeploymentJob(_ context.Context, p model.Project, job model.ComposeDeploymentJob, outbox model.ComposeDeploymentOutbox) error {
	if r.job.ID != uuid.Nil {
		return apperrors.ErrConflict
	}
	r.project = p
	r.job = job
	r.outbox = append(r.outbox, outbox)
	return nil
}

func (r *deploymentRepo) GetByID(_ context.Context, id uuid.UUID) (model.Project, error) {
	if err := popErr(&r.faults.GetProject); err != nil {
		return model.Project{}, err
	}
	if id != r.project.ID {
		return model.Project{}, apperrors.ErrNotFound
	}
	return r.project, nil
}

func (r *deploymentRepo) GetComposeDeploymentJob(_ context.Context, id uuid.UUID) (model.ComposeDeploymentJob, error) {
	if id != r.job.ID {
		return model.ComposeDeploymentJob{}, apperrors.ErrNotFound
	}
	return r.job, nil
}

func (r *deploymentRepo) GetActiveComposeDeploymentJobByProjectID(_ context.Context, projectID uuid.UUID) (model.ComposeDeploymentJob, error) {
	if projectID != r.job.ProjectID || !jobActive(r.job) {
		return model.ComposeDeploymentJob{}, apperrors.ErrNotFound
	}
	return r.job, nil
}

func (r *deploymentRepo) ListInterruptedComposeDeploymentJobs(context.Context) ([]model.ComposeDeploymentJob, error) {
	if !jobStatusIn(r.job, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStatusCanceling) {
		return nil, nil
	}
	return []model.ComposeDeploymentJob{r.job}, nil
}

func (r *deploymentRepo) StartComposeDeploymentJob(_ context.Context, id uuid.UUID) (model.ComposeDeploymentJob, bool, error) {
	if id != r.job.ID {
		return model.ComposeDeploymentJob{}, false, apperrors.ErrNotFound
	}
	if r.job.CancelRequested || r.job.Status != model.ComposeDeploymentStatusQueued {
		return r.job, false, nil
	}
	r.job.Status = model.ComposeDeploymentStatusRunning
	r.job.Attempts++
	return r.job, true, nil
}

func (r *deploymentRepo) RequeueComposeDeploymentJob(_ context.Context, id uuid.UUID) error {
	if err := popErr(&r.faults.RequeueJob); err != nil {
		return err
	}
	if id == r.job.ID && r.job.Status == model.ComposeDeploymentStatusRunning {
		r.job.Status = model.ComposeDeploymentStatusQueued
	}
	return nil
}

func (r *deploymentRepo) CompleteComposeDeploymentJob(_ context.Context, id uuid.UUID, status string, errorMsg *string) error {
	if err := popErr(&r.faults.CompleteJob); err != nil {
		return err
	}
	if id != r.job.ID {
		return apperrors.ErrNotFound
	}
	r.job.Status = status
	r.job.ErrorMessage = errorMsg
	r.completedStatus = status
	if errorMsg != nil {
		r.completedMessage = *errorMsg
	}
	return nil
}

func (r *deploymentRepo) RequestComposeDeploymentCancel(_ context.Context, projectID uuid.UUID) error {
	if projectID != r.job.ProjectID || !jobActive(r.job) {
		return apperrors.ErrNotFound
	}
	r.job.CancelRequested = true
	r.job.Status = model.ComposeDeploymentStatusCanceling
	return nil
}

func (r *deploymentRepo) UpdateStatus(_ context.Context, id uuid.UUID, status string, errMsg *string) error {
	if id != r.project.ID {
		return apperrors.ErrNotFound
	}
	r.project.Status = status
	r.project.ErrorMessage = errMsg
	r.projectStatuses = append(r.projectStatuses, status)
	return nil
}

func (r *deploymentRepo) SaveComposeDeploymentPlan(_ context.Context, id uuid.UUID, stage string, planJSON, resourceMapJSON []byte) error {
	if err := popErr(&r.faults.SavePlan); err != nil {
		return err
	}
	if id != r.job.ID || r.job.Status != model.ComposeDeploymentStatusRunning {
		return apperrors.ErrNotFound
	}
	r.job.Stage = stage
	r.job.PlanJSON = planJSON
	r.job.ResourceMapJSON = resourceMapJSON
	return nil
}

func (r *deploymentRepo) UpdateComposeDeploymentProgress(_ context.Context, id uuid.UUID, projectID uuid.UUID, projectStatus string, stage string, planJSON []byte, resourceMapJSON []byte) error {
	if id != r.job.ID || !jobStatusIn(r.job, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStatusCanceling) {
		return apperrors.ErrNotFound
	}
	saved := append([]byte(nil), resourceMapJSON...)
	r.job.Stage = stage
	r.job.PlanJSON = append([]byte(nil), planJSON...)
	r.job.ResourceMapJSON = saved
	if projectStatus != "" && projectID == r.project.ID {
		r.project.Status = projectStatus
		r.project.ErrorMessage = nil
	}
	r.savedResourceMaps = append(r.savedResourceMaps, saved)
	r.progress = append(r.progress, savedProgress{projectStatus: projectStatus, stage: stage, resources: saved})
	return nil
}

// ListActiveComposeDeploymentJobs lists the job while it is running or
// canceling with a durable stage, then stops the coordinator run that asked.
func (r *deploymentRepo) ListActiveComposeDeploymentJobs(context.Context, int) ([]model.ComposeDeploymentJob, error) {
	if r.cancel != nil {
		defer r.cancel()
	}
	if r.job.Stage == "" || !jobStatusIn(r.job, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStatusCanceling) {
		return nil, nil
	}
	return []model.ComposeDeploymentJob{r.job}, nil
}

func (r *deploymentRepo) SaveServiceGraph(_ context.Context, _ uuid.UUID, services []model.ProjectServiceNode) error {
	r.serviceGraph = services
	r.serviceGraphs++
	return nil
}

func (r *deploymentRepo) CountActiveComposeDeploymentsByOwner(_ context.Context, ownerID uuid.UUID) (int, error) {
	if r.job.OwnerID == ownerID && jobStatusIn(r.job, model.ComposeDeploymentStatusQueued, model.ComposeDeploymentStatusRunning) {
		return 1, nil
	}
	return 0, nil
}

func popErr(errs *[]error) error {
	if len(*errs) == 0 {
		return nil
	}
	err := (*errs)[0]
	*errs = (*errs)[1:]
	return err
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
	// removed holds containers whose queued delete finished; deleting one
	// again reports not found.
	removed map[uuid.UUID]bool
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
	c.containers[id] = model.Container{ID: id, ProjectID: params.ProjectID, Name: params.Name, Status: model.ContainerStatusCreated}
	return id, nil
}

// Delete queues the delete of an existing container; finishDeletes completes it.
func (c *coordinatorContainers) Delete(_ context.Context, id uuid.UUID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleted = append(c.deleted, id)
	if err, ok := c.deleteErrs[id]; ok {
		return err
	}
	if c.removed[id] {
		return apperrors.ErrNotFound
	}
	if item, ok := c.containers[id]; ok {
		item.Status = model.ContainerStatusDeleting
		c.containers[id] = item
	}
	return nil
}

// runQueuedStarts plays the lifecycle worker: every starting container runs.
func (c *coordinatorContainers) runQueuedStarts() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, item := range c.containers {
		if item.Status == model.ContainerStatusStarting {
			item.Status = model.ContainerStatusRunning
			item.DockerID = "docker-" + id.String()
			c.containers[id] = item
		}
	}
}

// finishDeletes plays the lifecycle worker: every deleting container is gone.
func (c *coordinatorContainers) finishDeletes() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.removed == nil {
		c.removed = make(map[uuid.UUID]bool)
	}
	for id, item := range c.containers {
		if item.Status == model.ContainerStatusDeleting {
			delete(c.containers, id)
			c.removed[id] = true
		}
	}
}

// byName returns the stored container with the given name.
func (c *coordinatorContainers) byName(t *testing.T, name string) model.Container {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, item := range c.containers {
		if item.Name == name {
			return item
		}
	}
	t.Fatalf("container %s not found", name)
	return model.Container{}
}

func (c *coordinatorContainers) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.containers)
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
	// removed holds volumes whose queued delete finished; deleting one again
	// reports not found.
	removed map[uuid.UUID]bool
}

func (v *coordinatorVolumes) create(_ context.Context, params model.VolumeCreateParams) (uuid.UUID, error) {
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
	id, err := v.create(ctx, model.VolumeCreateParams{ProjectID: &projectID, Name: name})
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
	if err, ok := v.deleteErrs[volumeID]; ok {
		return err
	}
	if v.removed[volumeID] {
		return apperrors.ErrNotFound
	}
	return nil
}

// finishDeletes plays the resource worker: every volume delete queued so far
// completes.
func (v *coordinatorVolumes) finishDeletes() {
	if v.removed == nil {
		v.removed = make(map[uuid.UUID]bool)
	}
	for _, id := range v.deleted {
		v.removed[id] = true
	}
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

// coordinatorBuilds records build reads, triggers, and cancellations. As a
// compose.BuildJobCreator it also stores the build records that the
// production LocalBuilderClient creates and cancels.
type coordinatorBuilds struct {
	builds    map[uuid.UUID]model.Build
	project   []model.Build
	triggered []string
	canceled  []uuid.UUID
	// deployments, when set, receives the cascade BuildService sends when it
	// cancels a project build.
	deployments buildDeploymentCanceler
}

type buildDeploymentCanceler interface {
	CancelDeploymentForBuild(ctx context.Context, projectID, buildID uuid.UUID) error
}

func (b *coordinatorBuilds) CreateBuildJob(context.Context, string, string, string, string, string, map[string]string, string) (uuid.UUID, uuid.UUID, error) {
	return uuid.Nil, uuid.Nil, errors.New("standalone builds are not part of a Compose deployment")
}

func (b *coordinatorBuilds) CreateProjectBuildJob(_ context.Context, projectID uuid.UUID, serviceName, _, _, _, _, _ string, _ map[string]string, _ string) (uuid.UUID, uuid.UUID, error) {
	build := model.Build{ID: uuid.New(), ProjectID: &projectID, ProjectServiceName: serviceName, ImageID: uuid.New(), Status: model.BuildStatusPending}
	if b.builds == nil {
		b.builds = make(map[uuid.UUID]model.Build)
	}
	b.builds[build.ID] = build
	b.project = append(b.project, build)
	b.triggered = append(b.triggered, serviceName)
	return build.ID, uuid.New(), nil
}

func (b *coordinatorBuilds) ReserveBuildArchive(context.Context, string) error { return nil }

func (b *coordinatorBuilds) ReleaseBuildArchive(context.Context, string) {}

// CancelBuildRecord cancels a build that has not finished and, like
// BuildService, cascades the cancellation to the build's deployment.
func (b *coordinatorBuilds) CancelBuildRecord(ctx context.Context, id uuid.UUID) error {
	b.canceled = append(b.canceled, id)
	build, ok := b.builds[id]
	if !ok {
		return apperrors.ErrNotFound
	}
	if !buildUnfinished(build) {
		return nil
	}
	build.Status = model.BuildStatusCanceled
	b.builds[id] = build
	if b.deployments != nil && build.ProjectID != nil {
		_ = b.deployments.CancelDeploymentForBuild(ctx, *build.ProjectID, build.ID) // BuildService logs and ignores cascade failures
	}
	return nil
}

func buildUnfinished(build model.Build) bool {
	return build.Status == model.BuildStatusPending || build.Status == model.BuildStatusRunning
}

// finish plays Builder: every unfinished build ends with status.
func (b *coordinatorBuilds) finish(status string) {
	for id, build := range b.builds {
		if buildUnfinished(build) {
			build.Status = status
			b.builds[id] = build
		}
	}
}

func (b *coordinatorBuilds) GetByID(_ context.Context, id uuid.UUID) (model.Build, error) {
	return b.builds[id], nil
}
func (b *coordinatorBuilds) GetByProjectID(context.Context, uuid.UUID) ([]model.Build, error) {
	return b.project, nil
}
func (b *coordinatorBuilds) TriggerBuild(_ context.Context, _ uuid.UUID, srv model.ComposeService, _ string) (uuid.UUID, error) {
	b.triggered = append(b.triggered, srv.Name)
	return uuid.New(), nil
}
func (b *coordinatorBuilds) CancelBuild(ctx context.Context, id uuid.UUID) error {
	return b.CancelBuildRecord(ctx, id)
}

// coordinatorResources lists the containers and volumes already persisted for
// a project.
type coordinatorResources struct {
	containers []model.Container
	volumes    []model.Volume
}

func (r *coordinatorResources) GetByProjectID(context.Context, uuid.UUID) ([]model.Container, error) {
	return r.containers, nil
}

func (r *coordinatorResources) GetVolumesByProjectID(context.Context, uuid.UUID) ([]model.Volume, error) {
	return r.volumes, nil
}

// coordinatorSources records staged Compose source objects the coordinator releases.
type coordinatorSources struct {
	released []string
}

func (s *coordinatorSources) ReleaseComposeSource(_ context.Context, key string) {
	s.released = append(s.released, key)
}

type coordinatorImages struct {
	deleted []uuid.UUID
}

func (i *coordinatorImages) Delete(_ context.Context, imageID uuid.UUID) error {
	i.deleted = append(i.deleted, imageID)
	return nil
}

type staticConfig struct {
	cfg config.SystemConfig
}

func (c staticConfig) Get() config.SystemConfig {
	return c.cfg
}
