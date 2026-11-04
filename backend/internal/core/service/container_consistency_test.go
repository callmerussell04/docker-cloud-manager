package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestContainerServiceExposeQueuesOperationWithoutDockerCall(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	store := newContainerStore()
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:   store,
		Docker:       coremocks.NewContainerDockerAPI(t),
		Config:       staticConfig{},
		HostDiskPath: "",
		Logger:       slog.Default(),
	})
	saveContainer(t, store, model.Container{
		ID:               containerID,
		OwnerID:          ownerID,
		DockerID:         "old-docker",
		Status:           model.ContainerStatusRunning,
		DockerGeneration: 1,
	})

	err := svc.Expose(accessscope.WithUserScope(context.Background(), ownerID, "", ""), containerID, "app", 8080)
	require.NoError(t, err)
	queued := requireQueued(t, store)
	require.Equal(t, model.OperationExpose, queued.op.Operation)
	require.Equal(t, model.ContainerStatusExposing, queued.container.Status)
	require.Contains(t, string(queued.outbox.Payload), `"domain_prefix":"app"`)
}

func TestContainerServiceCreateReportsDomainPrefixConflict(t *testing.T) {
	ownerID := uuid.New()
	repo := newContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	cfg := coremocks.NewConfigManager(t)
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:   repo,
		Docker:       dockerAPI,
		Config:       cfg,
		HostDiskPath: "",
		Logger:       slog.Default(),
	})

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
}

func TestContainerServiceExposeReportsDomainPrefixConflict(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	store := newContainerStore()
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:   store,
		Docker:       coremocks.NewContainerDockerAPI(t),
		Config:       staticConfig{},
		HostDiskPath: "",
		Logger:       slog.Default(),
	})
	saveContainer(t, store, model.Container{
		ID:       containerID,
		OwnerID:  ownerID,
		DockerID: "docker-id",
		Status:   model.ContainerStatusRunning,
	})
	saveContainer(t, store, model.Container{
		ID:           uuid.New(),
		OwnerID:      uuid.New(),
		DockerID:     "other-docker",
		DomainPrefix: "app",
		Status:       model.ContainerStatusRunning,
	})

	err := svc.Expose(accessscope.WithUserScope(context.Background(), ownerID, "", ""), containerID, "app", 8080)
	require.ErrorIs(t, err, apperrors.ErrAlreadyExists)
	require.Equal(t, "subdomain app.example.test is already in use", apperrors.SafeMessage(err))
	requireNothingQueued(t, store)
}

func TestContainerServiceExposeRejectsBlockedDomainPrefixAndAuditsFailure(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	store := newContainerStore()
	cfg := coremocks.NewConfigManager(t)
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:   store,
		Docker:       coremocks.NewContainerDockerAPI(t),
		Config:       cfg,
		HostDiskPath: "",
		Logger:       slog.Default(),
	})
	auditor := &recordingAuditRecorder{}
	svc.SetAuditRecorder(auditor)

	cfgValue := staticConfig{}.Get()
	cfgValue.BlockedDomainPrefixPatterns = []string{"preview-[0-9]+"}
	cfg.EXPECT().Get().Return(cfgValue).Maybe()
	saveContainer(t, store, model.Container{
		ID:           containerID,
		OwnerID:      ownerID,
		DockerID:     "docker-id",
		Name:         "web",
		DomainPrefix: "old",
		InternalPort: 80,
		Status:       model.ContainerStatusRunning,
	})

	err := svc.Expose(accessscope.WithUserScope(context.Background(), ownerID, "alice", ""), containerID, "preview-42", 8080)
	require.ErrorIs(t, err, apperrors.ErrBadRequest)
	require.Equal(t, `domain prefix "preview-42" is forbidden`, apperrors.SafeMessage(err))
	requireNothingQueued(t, store)

	require.Len(t, auditor.events, 1)
	event := auditor.events[0]
	require.Equal(t, auditlog.ActionContainerExpose, event.Action)
	require.Equal(t, auditlog.OutcomeFailure, event.Outcome)
	require.Equal(t, `domain prefix "preview-42" is forbidden`, event.ErrorCode)
	var details map[string]string
	require.NoError(t, json.Unmarshal([]byte(event.DetailsJSON), &details))
	require.Equal(t, "preview-42", details[auditlog.DetailDomainPrefix])
}

func TestContainerServiceDeleteQueuesOperationWithoutDockerCall(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	store := newContainerStore()
	svc := newTestContainerService(t, service.ContainerServiceDeps{Repository: store, Docker: coremocks.NewContainerDockerAPI(t), Config: staticConfig{}, HostDiskPath: "", Logger: slog.Default()})
	saveContainer(t, store, model.Container{ID: containerID, OwnerID: ownerID, DockerID: "missing-docker"})

	require.NoError(t, svc.Delete(accessscope.WithUserScope(context.Background(), ownerID, "", ""), containerID))
	queued := requireQueued(t, store)
	require.Equal(t, model.OperationDelete, queued.op.Operation)
	require.Equal(t, model.ContainerStatusDeleting, queued.container.Status)
}

func TestContainerServiceStopQueuesOperationWithoutDockerCall(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	store := newContainerStore()
	svc := newTestContainerService(t, service.ContainerServiceDeps{Repository: store, Docker: coremocks.NewContainerDockerAPI(t), Config: staticConfig{}, HostDiskPath: "", Logger: slog.Default()})
	saveContainer(t, store, model.Container{ID: containerID, OwnerID: ownerID, DockerID: "docker-id", Status: model.ContainerStatusRunning})

	err := svc.Stop(accessscope.WithUserScope(context.Background(), ownerID, "", ""), containerID)
	require.NoError(t, err)
	queued := requireQueued(t, store)
	require.Equal(t, model.OperationStop, queued.op.Operation)
	require.Equal(t, model.ContainerStatusStopping, queued.container.Status)
}

func TestTTLWorkerQueuesStopOperation(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := &ttlWorkerRepoFake{
		expired: []model.Container{{
			ID:      containerID,
			OwnerID: ownerID,
			Status:  model.ContainerStatusRunning,
		}},
		queuedCh: make(chan queuedContainerOperation, 1),
	}
	cfg := ttlWorkerConfig{ttlHours: 24, intervalSeconds: 1}
	worker := service.NewTTLWorker(repo, cfg, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go worker.Run(ctx)

	select {
	case queued := <-repo.queuedCh:
		require.Equal(t, model.OperationStop, queued.op.Operation)
		require.Equal(t, model.ContainerStatusStopping, queued.status)
		require.Equal(t, model.ContainerStatusExited, queued.desiredStatus)
		require.Equal(t, containerID, queued.op.ResourceID)
		require.Equal(t, ownerID, queued.op.OwnerID)
		require.Contains(t, string(queued.outbox.Payload), `"operation":"stop"`)
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("ttl worker did not queue stop operation")
	}
}

func TestTTLWorkerSkipsExpirationWhenTTLDisabled(t *testing.T) {
	repo := &ttlWorkerRepoFake{queuedCh: make(chan queuedContainerOperation, 1)}
	cfg := ttlWorkerConfig{ttlHours: 0, intervalSeconds: 1}
	worker := service.NewTTLWorker(repo, cfg, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())

	go worker.Run(ctx)
	time.Sleep(1100 * time.Millisecond)
	cancel()

	repo.mu.Lock()
	getExpiredCalls := repo.getExpiredCalls
	repo.mu.Unlock()
	require.Zero(t, getExpiredCalls)
	require.Empty(t, repo.queued)
}

func TestContainerServiceStartRejectsMissingContainerWithoutDockerCall(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := newContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	svc := newTestContainerService(t, service.ContainerServiceDeps{Repository: repo, Docker: dockerAPI, Config: staticConfig{}, HostDiskPath: "", Logger: slog.Default()})

	repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{ID: containerID, OwnerID: ownerID, DockerID: "missing-docker", Status: model.ContainerStatusMissing}, nil)

	err := svc.Start(accessscope.WithUserScope(context.Background(), ownerID, "", ""), containerID)
	require.Error(t, err)
}

func TestContainerServiceCreateQueuesOperation(t *testing.T) {
	ownerID := uuid.New()
	repo := coremocks.NewContainerRepository(t)
	lockHeld := false
	repo.EXPECT().AcquireOwnerCapacityLock(mock.Anything, ownerID).RunAndReturn(func(context.Context, uuid.UUID) (func(), error) {
		lockHeld = true
		return func() { lockHeld = false }, nil
	}).Once()
	repo.EXPECT().CountActiveCreateOperationsByOwner(mock.Anything, ownerID).Return(0, nil).Maybe()
	imageRepo := newContainerImageRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	metrics := coremocks.NewHostMetricsProvider(t)
	cfg := coremocks.NewConfigManager(t)
	users := coremocks.NewUserInfoProvider(t)
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:   repo,
		Images:       imageRepo,
		Docker:       dockerAPI,
		Metrics:      metrics,
		Config:       cfg,
		Users:        users,
		HostDiskPath: "",
		Logger:       slog.Default(),
	})
	ctx, cancel := context.WithCancel(accessscope.WithUserScope(context.Background(), ownerID, "", ""))
	defer cancel()
	cfgValue := staticConfig{}.Get()
	cfgValue.OvercommitFactor = 1.5
	cfgValue.ContainerTTLHours = 24

	cfg.EXPECT().Get().Return(cfgValue).Maybe()
	repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(1, nil).Maybe()
	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaRAMMB: 1024, QuotaDiskMB: 1024, QuotaCPU: 2}, nil)
	repo.EXPECT().GetUserReservedMemory(mock.Anything, ownerID).Return(int64(0), nil)
	repo.EXPECT().GetUserReservedCPU(mock.Anything, ownerID).Return(int64(0), nil)
	metrics.EXPECT().GetTotalMemory().Return(int64(8*1024*1024*1024), nil)
	repo.EXPECT().GetTotalSystemReservedMemory(mock.Anything).Return(int64(0), nil)
	repo.EXPECT().GetTotalSystemReservedCPU(mock.Anything).Return(int64(0), nil)
	metrics.EXPECT().GetLogicalCPUs().Return(int64(2), nil)
	imageRepo.EXPECT().List(mock.Anything, mock.MatchedBy(func(opts model.ListOptions) bool {
		return opts.OwnerID != nil && *opts.OwnerID == ownerID
	})).Return(nil, 0, nil)
	repo.EXPECT().SaveWithMountsOperationAndOutbox(mock.Anything, mock.MatchedBy(func(c model.Container) bool {
		return c.TTLDeadline == nil
	}), mock.Anything, mock.AnythingOfType("model.ResourceOperation"), mock.AnythingOfType("model.ContainerLifecycleOutbox"), false, false).Run(func(ctx context.Context, c model.Container, mounts []model.VolumeMount, op model.ResourceOperation, outbox model.ContainerLifecycleOutbox, lockOwner, lockCapacity bool) {
		require.True(t, lockHeld)
		require.Equal(t, ownerID, c.OwnerID)
		require.Equal(t, c.ID, op.ResourceID)
		require.Equal(t, op.ID, outbox.OperationID)
		require.Equal(t, c.ID, outbox.ContainerID)
		require.Equal(t, model.OperationStatusPending, op.Status)
		require.Equal(t, model.ContainerOutboxStatusPending, outbox.Status)
	}).Return(nil).Once()

	containerID, err := svc.Create(ctx, model.ContainerCreateParams{
		Name:              "web",
		ImageTag:          "nginx:latest",
		RequestedMemoryMB: 128,
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, containerID)
	require.False(t, lockHeld)
}

func TestContainerServiceQueuedCreateReturnsRequeueFailure(t *testing.T) {
	ctx := context.Background()
	ownerID, containerID, operationID := uuid.New(), uuid.New(), uuid.New()
	store := newContainerStore()
	require.NoError(t, store.SaveWithMountsOperationAndOutbox(ctx,
		model.Container{ID: containerID, OwnerID: ownerID, Name: "web", ImageTag: "nginx:latest", Status: model.ContainerStatusPending, DesiredStatus: model.ContainerStatusCreated},
		nil,
		model.ResourceOperation{ID: operationID, ResourceType: model.ResourceTypeContainer, ResourceID: containerID, OwnerID: ownerID, Operation: model.OperationCreate, Status: model.OperationStatusPending},
		model.ContainerLifecycleOutbox{ID: uuid.New(), OperationID: operationID, ContainerID: containerID, Status: model.ContainerOutboxStatusPending},
		false, false))
	statusErr := apperrors.Wrap(apperrors.ErrUnavailable, "database unavailable", errors.New("connection reset"))
	requeueErr := errors.New("requeue failed")
	store.SetFaults(coretest.ContainerStoreFaults{
		UpdateStatus:     coretest.FailAlways(statusErr),
		RequeueOperation: coretest.FailAlways(requeueErr),
	})
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository: store,
		Docker:     coremocks.NewContainerDockerAPI(t),
		Config:     staticConfig{},
		Logger:     slog.Default(),
	})

	err := handleQueuedCreate(svc, operationID, containerID)
	require.ErrorIs(t, err, statusErr)
	require.ErrorIs(t, err, requeueErr)
}

func TestContainerServiceExposePreservesNetworkAlias(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	store := newContainerStore()
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:   store,
		Docker:       coremocks.NewContainerDockerAPI(t),
		Config:       staticConfig{},
		HostDiskPath: "",
		Logger:       slog.Default(),
	})
	auditor := &recordingAuditRecorder{}
	svc.SetAuditRecorder(auditor)
	projectID := uuid.New()

	saveContainer(t, store, model.Container{
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
	})

	require.NoError(t, svc.Expose(accessscope.WithUserScope(context.Background(), ownerID, "alice", ""), containerID, "app", 8080))
	require.Contains(t, string(requireQueued(t, store).outbox.Payload), `"previous_status":"created"`)
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
	repo := newContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	metrics := coremocks.NewHostMetricsProvider(t)
	cfg := coremocks.NewConfigManager(t)
	done := make(chan struct{})
	var updates int
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:   repo,
		Docker:       dockerAPI,
		Metrics:      metrics,
		Config:       cfg,
		HostDiskPath: "",
		Logger:       slog.Default(),
	})

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
	repo := newContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	metrics := coremocks.NewHostMetricsProvider(t)
	cfg := coremocks.NewConfigManager(t)
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:   repo,
		Docker:       dockerAPI,
		Metrics:      metrics,
		Config:       cfg,
		HostDiskPath: "",
		Logger:       slog.Default(),
	})
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
	repo := newContainerRepository(t)
	imageRepo := newContainerImageRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	metrics := coremocks.NewHostMetricsProvider(t)
	cfg := coremocks.NewConfigManager(t)
	users := coremocks.NewUserInfoProvider(t)
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:   repo,
		Images:       imageRepo,
		Docker:       dockerAPI,
		Metrics:      metrics,
		Config:       cfg,
		Users:        users,
		HostDiskPath: "",
		Logger:       slog.Default(),
	})
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")

	cfg.EXPECT().Get().Return(staticConfig{}.Get()).Maybe()
	repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(0, nil).Maybe()
	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaRAMMB: 1024, QuotaDiskMB: 1024, QuotaCPU: 0.1}, nil)
	repo.EXPECT().GetUserReservedMemory(mock.Anything, ownerID).Return(int64(0), nil)
	repo.EXPECT().GetUserReservedCPU(mock.Anything, ownerID).Return(int64(0), nil)

	_, err := svc.Create(ctx, model.ContainerCreateParams{Name: "web", ImageTag: "nginx:latest"})
	require.ErrorIs(t, err, apperrors.ErrQuotaExceeded)
}

func TestContainerServiceCreateRejectsHostCPUCapacity(t *testing.T) {
	ownerID := uuid.New()
	repo := newContainerRepository(t)
	imageRepo := newContainerImageRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	metrics := coremocks.NewHostMetricsProvider(t)
	cfg := coremocks.NewConfigManager(t)
	users := coremocks.NewUserInfoProvider(t)
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:   repo,
		Images:       imageRepo,
		Docker:       dockerAPI,
		Metrics:      metrics,
		Config:       cfg,
		Users:        users,
		HostDiskPath: "",
		Logger:       slog.Default(),
	})
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
	cfgValue := staticConfig{}.Get()
	cfgValue.CPUOvercommitFactor = 0.2

	cfg.EXPECT().Get().Return(cfgValue).Maybe()
	repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(0, nil).Maybe()
	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaRAMMB: 1024, QuotaDiskMB: 1024, QuotaCPU: 2}, nil)
	repo.EXPECT().GetUserReservedMemory(mock.Anything, ownerID).Return(int64(0), nil)
	repo.EXPECT().GetUserReservedCPU(mock.Anything, ownerID).Return(int64(0), nil)
	repo.EXPECT().GetTotalSystemReservedMemory(mock.Anything).Return(int64(0), nil)
	repo.EXPECT().GetTotalSystemReservedCPU(mock.Anything).Return(int64(0), nil)
	metrics.EXPECT().GetTotalMemory().Return(int64(8*1024*1024*1024), nil)
	metrics.EXPECT().GetLogicalCPUs().Return(int64(1), nil)

	_, err := svc.Create(ctx, model.ContainerCreateParams{Name: "web", ImageTag: "nginx:latest"})
	require.ErrorIs(t, err, apperrors.ErrHostExhausted)
}

type queuedContainerOperation struct {
	status        string
	desiredStatus string
	op            model.ResourceOperation
	outbox        model.ContainerLifecycleOutbox
}

type ttlWorkerRepoFake struct {
	mu              sync.Mutex
	expired         []model.Container
	queued          []queuedContainerOperation
	queuedCh        chan queuedContainerOperation
	getExpiredCalls int
}

func (r *ttlWorkerRepoFake) GetExpired(ctx context.Context) ([]model.Container, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.getExpiredCalls++
	return append([]model.Container(nil), r.expired...), nil
}

func (r *ttlWorkerRepoFake) QueueContainerOperation(ctx context.Context, id uuid.UUID, status string, desiredStatus string, op model.ResourceOperation, outbox model.ContainerLifecycleOutbox) error {
	queued := queuedContainerOperation{
		status:        status,
		desiredStatus: desiredStatus,
		op:            op,
		outbox:        outbox,
	}
	r.mu.Lock()
	r.queued = append(r.queued, queued)
	r.mu.Unlock()
	if r.queuedCh != nil {
		select {
		case r.queuedCh <- queued:
		default:
		}
	}
	return nil
}

func containerLifecycleMessage(operationID, containerID, ownerID uuid.UUID, operation string) queue.ContainerLifecycleMessage {
	return queue.ContainerLifecycleMessage{
		OperationID:    operationID.String(),
		ContainerID:    containerID.String(),
		OwnerID:        ownerID.String(),
		Operation:      operation,
		PreviousStatus: model.ContainerStatusExited,
		CreatedAt:      time.Now().Unix(),
	}
}

type recordingAuditRecorder struct {
	events []model.AuditEvent
}

func (r *recordingAuditRecorder) RecordAuditEvent(ctx context.Context, event model.AuditEvent) error {
	r.events = append(r.events, event)
	return nil
}

type staticConfig struct{}

type ttlWorkerConfig struct {
	ttlHours        int64
	intervalSeconds int64
}

func (c ttlWorkerConfig) Get() config.SystemConfig {
	return config.SystemConfig{
		ContainerTTLHours:        c.ttlHours,
		TTLWorkerIntervalSeconds: c.intervalSeconds,
	}
}

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
