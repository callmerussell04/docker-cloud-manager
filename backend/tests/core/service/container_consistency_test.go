package service_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	. "github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/auditlog"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestContainerServiceExposeQueuesOperationWithoutDockerCall(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := newContainerLifecycleRepoMock(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	cfg := coremocks.NewConfigManager(t)
	svc := NewContainerService(repo, nil, nil, dockerAPI, nil, cfg, nil, "", slog.Default())

	cfg.EXPECT().Get().Return(staticConfig{}.Get()).Maybe()
	repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{
		ID:               containerID,
		OwnerID:          ownerID,
		DockerID:         "old-docker",
		Status:           model.ContainerStatusRunning,
		DockerGeneration: 1,
	}, nil)
	repo.EXPECT().CheckDomainPrefixExists(mock.Anything, "app").Return(false, nil)

	err := svc.Expose(accessscope.WithUserScope(context.Background(), ownerID, "", ""), containerID, "app", 8080)
	require.NoError(t, err)
	require.Len(t, repo.queued, 1)
	require.Equal(t, model.OperationExpose, repo.queued[0].op.Operation)
	require.Equal(t, model.ContainerStatusExposing, repo.queued[0].status)
	require.Contains(t, string(repo.queued[0].outbox.Payload), `"domain_prefix":"app"`)
	dockerAPI.AssertNotCalled(t, "InspectContainer", mock.Anything, mock.Anything)
	dockerAPI.AssertNotCalled(t, "CreateContainer", mock.Anything, mock.Anything)
}

func TestContainerServiceCreateReportsDomainPrefixConflict(t *testing.T) {
	ownerID := uuid.New()
	repo := coremocks.NewContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	cfg := coremocks.NewConfigManager(t)
	svc := NewContainerService(repo, nil, nil, dockerAPI, nil, cfg, nil, "", slog.Default())

	cfg.EXPECT().Get().Return(staticConfig{}.Get()).Maybe()
	repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(0, nil)
	repo.EXPECT().CheckDomainPrefixExists(mock.Anything, "app").Return(true, nil)

	_, err := svc.Create(accessscope.WithUserScope(context.Background(), ownerID, "", ""), model.ContainerCreateParams{
		Name:         "web",
		ImageTag:     "nginx:latest",
		DomainPrefix: "app",
		InternalPort: 8080,
	})
	require.ErrorIs(t, err, apperrors.ErrAlreadyExists)
	require.Equal(t, "subdomain app.example.test is already in use", apperrors.SafeMessage(err))
	dockerAPI.AssertNotCalled(t, "CreateContainer", mock.Anything, mock.Anything)
}

func TestContainerServiceExposeReportsDomainPrefixConflict(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := newContainerLifecycleRepoMock(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	cfg := coremocks.NewConfigManager(t)
	svc := NewContainerService(repo, nil, nil, dockerAPI, nil, cfg, nil, "", slog.Default())

	cfg.EXPECT().Get().Return(staticConfig{}.Get()).Maybe()
	repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{
		ID:       containerID,
		OwnerID:  ownerID,
		DockerID: "docker-id",
		Status:   model.ContainerStatusRunning,
	}, nil)
	repo.EXPECT().CheckDomainPrefixExists(mock.Anything, "app").Return(true, nil)

	err := svc.Expose(accessscope.WithUserScope(context.Background(), ownerID, "", ""), containerID, "app", 8080)
	require.ErrorIs(t, err, apperrors.ErrAlreadyExists)
	require.Equal(t, "subdomain app.example.test is already in use", apperrors.SafeMessage(err))
	require.Empty(t, repo.queued)
	dockerAPI.AssertNotCalled(t, "CreateContainer", mock.Anything, mock.Anything)
}

func TestContainerServiceDeleteQueuesOperationWithoutDockerCall(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := newContainerLifecycleRepoMock(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	svc := NewContainerService(repo, nil, nil, dockerAPI, nil, staticConfig{}, nil, "", slog.Default())

	repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{ID: containerID, OwnerID: ownerID, DockerID: "missing-docker"}, nil)

	require.NoError(t, svc.Delete(accessscope.WithUserScope(context.Background(), ownerID, "", ""), containerID))
	require.Len(t, repo.queued, 1)
	require.Equal(t, model.OperationDelete, repo.queued[0].op.Operation)
	require.Equal(t, model.ContainerStatusDeleting, repo.queued[0].status)
	dockerAPI.AssertNotCalled(t, "RemoveContainer", mock.Anything, mock.Anything, mock.Anything)
}

func TestContainerServiceStopQueuesOperationWithoutDockerCall(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := newContainerLifecycleRepoMock(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	svc := NewContainerService(repo, nil, nil, dockerAPI, nil, staticConfig{}, nil, "", slog.Default())

	repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{ID: containerID, OwnerID: ownerID, DockerID: "docker-id", Status: model.ContainerStatusRunning}, nil)

	err := svc.Stop(accessscope.WithUserScope(context.Background(), ownerID, "", ""), containerID)
	require.NoError(t, err)
	require.Len(t, repo.queued, 1)
	require.Equal(t, model.OperationStop, repo.queued[0].op.Operation)
	require.Equal(t, model.ContainerStatusStopping, repo.queued[0].status)
	dockerAPI.AssertNotCalled(t, "StopContainer", mock.Anything, mock.Anything, mock.Anything)
}

func TestContainerServiceStartRejectsMissingContainerWithoutDockerCall(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := coremocks.NewContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	svc := NewContainerService(repo, nil, nil, dockerAPI, nil, staticConfig{}, nil, "", slog.Default())

	repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{ID: containerID, OwnerID: ownerID, DockerID: "missing-docker", Status: model.ContainerStatusMissing}, nil)

	err := svc.Start(accessscope.WithUserScope(context.Background(), ownerID, "", ""), containerID)
	require.Error(t, err)
	dockerAPI.AssertNotCalled(t, "StartContainer", mock.Anything, mock.Anything)
}

func TestContainerServiceCreateQueuesOperation(t *testing.T) {
	ownerID := uuid.New()
	repo := newContainerCreateStateRepoMock(t)
	imageRepo := coremocks.NewContainerImageRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	metrics := coremocks.NewHostMetricsProvider(t)
	cfg := coremocks.NewConfigManager(t)
	users := coremocks.NewUserInfoProvider(t)
	svc := NewContainerService(repo, nil, imageRepo, dockerAPI, metrics, cfg, users, "", slog.Default())
	ctx, cancel := context.WithCancel(accessscope.WithUserScope(context.Background(), ownerID, "", ""))
	defer cancel()
	cfgValue := staticConfig{}.Get()
	cfgValue.OvercommitFactor = 1.5

	cfg.EXPECT().Get().Return(cfgValue).Maybe()
	repo.ContainerRepository.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(1, nil).Maybe()
	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaRAMMB: 1024, QuotaDiskMB: 1024, QuotaCPU: 2}, nil)
	repo.ContainerRepository.EXPECT().GetUserReservedMemory(mock.Anything, ownerID).Return(int64(0), nil)
	repo.ContainerRepository.EXPECT().GetUserReservedCPU(mock.Anything, ownerID).Return(int64(0), nil)
	metrics.EXPECT().GetTotalMemory().Return(int64(8*1024*1024*1024), nil)
	repo.ContainerRepository.EXPECT().GetTotalSystemReservedMemory(mock.Anything).Return(int64(0), nil)
	repo.ContainerRepository.EXPECT().GetTotalSystemReservedCPU(mock.Anything).Return(int64(0), nil)
	metrics.EXPECT().GetLogicalCPUs().Return(int64(2), nil)
	imageRepo.EXPECT().List(mock.Anything, mock.MatchedBy(func(opts model.ListOptions) bool {
		return opts.OwnerID != nil && *opts.OwnerID == ownerID
	})).Return(nil, 0, nil)
	repo.ContainerCreateRepository.EXPECT().SaveWithMountsAndOperation(mock.Anything, mock.AnythingOfType("model.Container"), mock.Anything, mock.AnythingOfType("model.ResourceOperation"), false, false).Return(nil)

	containerID, err := svc.Create(ctx, model.ContainerCreateParams{
		Name:              "web",
		ImageTag:          "nginx:latest",
		RequestedMemoryMB: 128,
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, containerID)
}

func TestContainerServiceExposePreservesNetworkAlias(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := newContainerLifecycleRepoMock(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	cfg := coremocks.NewConfigManager(t)
	svc := NewContainerService(repo, nil, nil, dockerAPI, nil, cfg, nil, "", slog.Default())
	auditor := &recordingAuditRecorder{}
	svc.SetAuditRecorder(auditor)
	projectID := uuid.New()

	cfg.EXPECT().Get().Return(staticConfig{}.Get()).Maybe()
	repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{
		ID:               containerID,
		OwnerID:          ownerID,
		DockerID:         "old-docker",
		Name:             "project_api",
		ProjectID:        &projectID,
		NetworkAlias:     "api",
		DomainPrefix:     "old",
		InternalPort:     80,
		Status:           model.ContainerStatusCreated,
		DockerGeneration: 1,
	}, nil)
	repo.EXPECT().CheckDomainPrefixExists(mock.Anything, "app").Return(false, nil)

	require.NoError(t, svc.Expose(accessscope.WithUserScope(context.Background(), ownerID, "alice", ""), containerID, "app", 8080))
	require.Len(t, repo.queued, 1)
	require.Contains(t, string(repo.queued[0].outbox.Payload), `"previous_status":"created"`)
	require.Len(t, auditor.events, 1)
	event := auditor.events[0]
	require.Equal(t, auditlog.ActionContainerExpose, event.Action)
	require.Equal(t, auditlog.OutcomeSuccess, event.Outcome)
	require.Equal(t, containerID.String(), event.ResourceID)
	require.Equal(t, "project_api", event.ResourceName)
	require.NotNil(t, event.OwnerID)
	require.Equal(t, ownerID, *event.OwnerID)
	require.Equal(t, "alice", event.ActorUsername)
	var details map[string]string
	require.NoError(t, json.Unmarshal([]byte(event.DetailsJSON), &details))
	require.Equal(t, "app", details[auditlog.DetailDomainPrefix])
	require.Equal(t, "app.example.test", details[auditlog.DetailFullDomain])
	require.Equal(t, "8080", details[auditlog.DetailInternalPort])
	require.Equal(t, "old", details[auditlog.DetailPreviousDomainPrefix])
	require.Equal(t, "80", details[auditlog.DetailPreviousInternalPort])
	require.Equal(t, projectID.String(), details[auditlog.DetailProjectID])
}

func TestContainerServiceRebalancerCoalescesQueuedSignals(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := coremocks.NewContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	metrics := coremocks.NewHostMetricsProvider(t)
	cfg := coremocks.NewConfigManager(t)
	done := make(chan struct{})
	var updates int
	svc := NewContainerService(repo, nil, nil, dockerAPI, metrics, cfg, nil, "", slog.Default())

	repo.EXPECT().GetRunning(mock.Anything).Return([]model.Container{{ID: uuid.New(), DockerID: "docker-id", BaseMemoryReservation: 128, BaseCPUReservation: 250}}, nil)
	metrics.EXPECT().GetTotalMemory().Return(int64(1024), nil)
	metrics.EXPECT().GetLogicalCPUs().Return(int64(1), nil)
	cfg.EXPECT().Get().Return(staticConfig{}.Get()).Maybe()
	dockerAPI.EXPECT().
		UpdateContainerResources(mock.Anything, "docker-id", mock.Anything).
		Run(func(ctx context.Context, dockerID string, resources model.ContainerResourceUpdate) {
			updates++
			select {
			case <-done:
			default:
				close(done)
			}
		}).
		Return(nil)

	for i := 0; i < 100; i++ {
		svc.RequestRebalance()
	}
	go svc.RunRebalancer(ctx)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("rebalancer did not process queued signal")
	}
	cancel()
	require.Equal(t, 1, updates)
}

func TestContainerServiceRebalanceAppliesMemoryAndCPUBurst(t *testing.T) {
	repo := coremocks.NewContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	metrics := coremocks.NewHostMetricsProvider(t)
	cfg := coremocks.NewConfigManager(t)
	svc := NewContainerService(repo, nil, nil, dockerAPI, metrics, cfg, nil, "", slog.Default())
	cfgValue := staticConfig{}.Get()
	cfgValue.MaxBurstMultiplier = 3
	cfgValue.MaxCPUBurstMultiplier = 4
	cfgValue.ContainerCPUPeriod = 100000

	repo.EXPECT().GetRunning(mock.Anything).Return([]model.Container{
		{ID: uuid.New(), DockerID: "docker-a", BaseMemoryReservation: 100, BaseCPUReservation: 250},
		{ID: uuid.New(), DockerID: "docker-b", BaseMemoryReservation: 100, BaseCPUReservation: 250},
	}, nil)
	metrics.EXPECT().GetTotalMemory().Return(int64(1000), nil)
	metrics.EXPECT().GetLogicalCPUs().Return(int64(2), nil)
	cfg.EXPECT().Get().Return(cfgValue).Maybe()

	dockerAPI.EXPECT().
		UpdateContainerResources(mock.Anything, mock.AnythingOfType("string"), mock.MatchedBy(func(resources model.ContainerResourceUpdate) bool {
			return resources.MemoryLimitBytes == 300 &&
				resources.MemoryReservation == 100 &&
				resources.CPUQuota == 100000 &&
				resources.CPUPeriod == 100000 &&
				resources.CPUShares == 1024
		})).
		Return(nil).
		Twice()

	svc.RebalanceResources(context.Background())
}

func TestContainerServiceCreateRejectsCPUQuotaExceeded(t *testing.T) {
	ownerID := uuid.New()
	repo := newContainerCreateStateRepoMock(t)
	imageRepo := coremocks.NewContainerImageRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	metrics := coremocks.NewHostMetricsProvider(t)
	cfg := coremocks.NewConfigManager(t)
	users := coremocks.NewUserInfoProvider(t)
	svc := NewContainerService(repo, nil, imageRepo, dockerAPI, metrics, cfg, users, "", slog.Default())
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")

	cfg.EXPECT().Get().Return(staticConfig{}.Get()).Maybe()
	repo.ContainerRepository.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(0, nil).Maybe()
	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaRAMMB: 1024, QuotaDiskMB: 1024, QuotaCPU: 0.1}, nil)
	repo.ContainerRepository.EXPECT().GetUserReservedMemory(mock.Anything, ownerID).Return(int64(0), nil)
	repo.ContainerRepository.EXPECT().GetUserReservedCPU(mock.Anything, ownerID).Return(int64(0), nil)

	_, err := svc.Create(ctx, model.ContainerCreateParams{Name: "web", ImageTag: "nginx:latest"})
	require.ErrorIs(t, err, apperrors.ErrQuotaExceeded)
}

func TestContainerServiceCreateRejectsHostCPUCapacity(t *testing.T) {
	ownerID := uuid.New()
	repo := newContainerCreateStateRepoMock(t)
	imageRepo := coremocks.NewContainerImageRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	metrics := coremocks.NewHostMetricsProvider(t)
	cfg := coremocks.NewConfigManager(t)
	users := coremocks.NewUserInfoProvider(t)
	svc := NewContainerService(repo, nil, imageRepo, dockerAPI, metrics, cfg, users, "", slog.Default())
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
	cfgValue := staticConfig{}.Get()
	cfgValue.CPUOvercommitFactor = 0.2

	cfg.EXPECT().Get().Return(cfgValue).Maybe()
	repo.ContainerRepository.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(0, nil).Maybe()
	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaRAMMB: 1024, QuotaDiskMB: 1024, QuotaCPU: 2}, nil)
	repo.ContainerRepository.EXPECT().GetUserReservedMemory(mock.Anything, ownerID).Return(int64(0), nil)
	repo.ContainerRepository.EXPECT().GetUserReservedCPU(mock.Anything, ownerID).Return(int64(0), nil)
	repo.ContainerRepository.EXPECT().GetTotalSystemReservedMemory(mock.Anything).Return(int64(0), nil)
	repo.ContainerRepository.EXPECT().GetTotalSystemReservedCPU(mock.Anything).Return(int64(0), nil)
	metrics.EXPECT().GetTotalMemory().Return(int64(8*1024*1024*1024), nil)
	metrics.EXPECT().GetLogicalCPUs().Return(int64(1), nil)

	_, err := svc.Create(ctx, model.ContainerCreateParams{Name: "web", ImageTag: "nginx:latest"})
	require.ErrorIs(t, err, apperrors.ErrHostExhausted)
}

type containerStateRepoMock struct {
	*coremocks.ContainerRepository
	*coremocks.ContainerStateRepository
}

func newContainerStateRepoMock(t *testing.T) *containerStateRepoMock {
	t.Helper()
	return &containerStateRepoMock{
		ContainerRepository:      coremocks.NewContainerRepository(t),
		ContainerStateRepository: coremocks.NewContainerStateRepository(t),
	}
}

type queuedContainerOperation struct {
	status        string
	desiredStatus string
	op            model.ResourceOperation
	outbox        model.ContainerLifecycleOutbox
}

type containerLifecycleRepoMock struct {
	*coremocks.ContainerRepository
	queued []queuedContainerOperation
}

func newContainerLifecycleRepoMock(t *testing.T) *containerLifecycleRepoMock {
	t.Helper()
	return &containerLifecycleRepoMock{ContainerRepository: coremocks.NewContainerRepository(t)}
}

func (r *containerLifecycleRepoMock) QueueContainerOperation(ctx context.Context, id uuid.UUID, status string, desiredStatus string, op model.ResourceOperation, outbox model.ContainerLifecycleOutbox) error {
	r.queued = append(r.queued, queuedContainerOperation{
		status:        status,
		desiredStatus: desiredStatus,
		op:            op,
		outbox:        outbox,
	})
	return nil
}

type containerCreateStateRepoMock struct {
	*coremocks.ContainerRepository
	*coremocks.ContainerCreateRepository
	*coremocks.ContainerStateRepository
}

func newContainerCreateStateRepoMock(t *testing.T) *containerCreateStateRepoMock {
	t.Helper()
	return &containerCreateStateRepoMock{
		ContainerRepository:       coremocks.NewContainerRepository(t),
		ContainerCreateRepository: coremocks.NewContainerCreateRepository(t),
		ContainerStateRepository:  coremocks.NewContainerStateRepository(t),
	}
}

func (r *containerCreateStateRepoMock) SaveWithMountsOperationAndOutbox(ctx context.Context, c model.Container, mounts []model.VolumeMount, op model.ResourceOperation, outbox model.ContainerLifecycleOutbox, lockOwner, lockCapacity bool) error {
	return r.ContainerCreateRepository.SaveWithMountsAndOperation(ctx, c, mounts, op, lockOwner, lockCapacity)
}

type recordingAuditRecorder struct {
	events []model.AuditEvent
}

func (r *recordingAuditRecorder) RecordAuditEvent(ctx context.Context, event model.AuditEvent) error {
	r.events = append(r.events, event)
	return nil
}

type staticConfig struct{}

func (staticConfig) Get() config.SystemConfig {
	return config.SystemConfig{
		ReservedSystemMemory:                 0,
		OvercommitFactor:                     1.5,
		MaxBurstMultiplier:                   4,
		DefaultCPUReservation:                250,
		ReservedSystemCPU:                    0,
		CPUOvercommitFactor:                  4,
		MaxCPUBurstMultiplier:                4,
		ContainerCPUPeriod:                   100000,
		DefaultCPUShares:                     1024,
		HighLoadCPUShares:                    512,
		HighLoadContainerCount:               5,
		ContainerStopTimeout:                 1,
		BaseDomain:                           "example.test",
		ContainerPidsLimit:                   256,
		ContainerMemorySwapMultiplier:        2,
		ProxyNetworkName:                     "proxy_net",
		EventSyncIntervalSeconds:             30,
		EventReconnectDelaySeconds:           5,
		BuildOutboxIntervalSeconds:           1,
		BuildOutboxBatchSize:                 10,
		ContainerCreateWorkerCount:           2,
		ContainerCreateMaxAttempts:           3,
		ContainerCreateTimeoutMinutes:        30,
		MaxQueuedContainerCreatesPerUser:     5,
		ContainerCreateOutboxIntervalSeconds: 1,
		ContainerCreateOutboxBatchSize:       10,
		MaxContainersPerUser:                 10,
		MaxVolumesPerUser:                    10,
		ImageBuildsEnabled:                   true,
		GitSourcesEnabled:                    true,
		ComposeDependencyWaitTimeoutMinutes:  1,
		ComposeDependencyPollIntervalSeconds: 1,
		ComposeCoordinatorIntervalSeconds:    2,
	}
}
