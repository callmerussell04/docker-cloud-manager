package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
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
		Config:       testConfig(),
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
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:   repo,
		Docker:       dockerAPI,
		Config:       testConfig(),
		HostDiskPath: "",
		Logger:       slog.Default(),
	})

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
		Config:       testConfig(),
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
	cfgValue := testSystemConfig()
	cfgValue.BlockedDomainPrefixPatterns = []string{"preview-[0-9]+"}
	cfg := coretest.StaticConfig{Config: cfgValue}
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:   store,
		Docker:       coremocks.NewContainerDockerAPI(t),
		Config:       cfg,
		HostDiskPath: "",
		Logger:       slog.Default(),
	})
	auditor := &recordingAuditRecorder{}
	svc.SetAuditRecorder(auditor)

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
	svc := newTestContainerService(t, service.ContainerServiceDeps{Repository: store, Docker: coremocks.NewContainerDockerAPI(t), Config: testConfig(), HostDiskPath: "", Logger: slog.Default()})
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
	svc := newTestContainerService(t, service.ContainerServiceDeps{Repository: store, Docker: coremocks.NewContainerDockerAPI(t), Config: testConfig(), HostDiskPath: "", Logger: slog.Default()})
	saveContainer(t, store, model.Container{ID: containerID, OwnerID: ownerID, DockerID: "docker-id", Status: model.ContainerStatusRunning})

	err := svc.Stop(accessscope.WithUserScope(context.Background(), ownerID, "", ""), containerID)
	require.NoError(t, err)
	queued := requireQueued(t, store)
	require.Equal(t, model.OperationStop, queued.op.Operation)
	require.Equal(t, model.ContainerStatusStopping, queued.container.Status)
}

func TestTTLWorkerQueuesStopOperation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
		cfg := coretest.StaticConfig{Config: config.SystemConfig{ContainerTTLHours: 24, TTLWorkerIntervalSeconds: 1}}
		stop := startInBubble(t, service.NewTTLWorker(repo, cfg, slog.Default()).Run)
		defer stop()

		time.Sleep(time.Second)
		synctest.Wait()
		select {
		case queued := <-repo.queuedCh:
			require.Equal(t, model.OperationStop, queued.op.Operation)
			require.Equal(t, model.ContainerStatusStopping, queued.status)
			require.Equal(t, model.ContainerStatusExited, queued.desiredStatus)
			require.Equal(t, containerID, queued.op.ResourceID)
			require.Equal(t, ownerID, queued.op.OwnerID)
			require.Contains(t, string(queued.outbox.Payload), `"operation":"stop"`)
		default:
			t.Fatal("ttl worker did not queue a stop operation within one interval")
		}
	})
}

func TestTTLWorkerSkipsExpirationWhenTTLDisabled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo := &ttlWorkerRepoFake{queuedCh: make(chan queuedContainerOperation, 1)}
		cfg := coretest.StaticConfig{Config: config.SystemConfig{ContainerTTLHours: 0, TTLWorkerIntervalSeconds: 1}}
		stop := startInBubble(t, service.NewTTLWorker(repo, cfg, slog.Default()).Run)

		time.Sleep(10 * time.Second)
		synctest.Wait()
		stop()

		require.Zero(t, repo.getExpiredCalls)
		require.Empty(t, repo.queued)
	})
}

func TestContainerServiceStartRejectsMissingContainerWithoutDockerCall(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := newContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	svc := newTestContainerService(t, service.ContainerServiceDeps{Repository: repo, Docker: dockerAPI, Config: testConfig(), HostDiskPath: "", Logger: slog.Default()})

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
	cfgValue := testSystemConfig()
	cfgValue.OvercommitFactor = 1.5
	cfgValue.ContainerTTLHours = 24
	cfg := coretest.StaticConfig{Config: cfgValue}
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
		Config:     testConfig(),
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
		Config:       testConfig(),
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

// testSystemConfig is the runtime config Core service tests start from.
func testSystemConfig() config.SystemConfig {
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
		ComposeCoordinatorIntervalSeconds:    2,
	}
}

// testConfig serves testSystemConfig through the shared static config double.
func testConfig() coretest.StaticConfig {
	return coretest.StaticConfig{Config: testSystemConfig()}
}
