package service_test

import (
	"context"
	"encoding/json"
	"errors"
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
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestContainerServiceExposeFailureKeepsOldContainer(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := coremocks.NewContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	cfg := coremocks.NewConfigManager(t)
	svc := NewContainerService(repo, nil, nil, dockerAPI, nil, cfg, nil, "", slog.Default())
	createErr := errors.New("create failed")

	cfg.EXPECT().Get().Return(staticConfig{}.Get()).Maybe()
	repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{
		ID:               containerID,
		OwnerID:          ownerID,
		DockerID:         "old-docker",
		Status:           model.ContainerStatusRunning,
		DockerGeneration: 1,
	}, nil)
	repo.EXPECT().CheckDomainPrefixExists(mock.Anything, "app").Return(false, nil)
	dockerAPI.EXPECT().InspectContainer(mock.Anything, "old-docker").Return(model.ContainerInspection{
		Name:              "/usr_old",
		Image:             "nginx:latest",
		MemoryLimitBytes:  128,
		MemoryReservation: 128,
		CPUShares:         1024,
		State:             model.ContainerState{Running: true},
	}, nil)
	dockerAPI.EXPECT().CreateContainer(mock.Anything, mock.AnythingOfType("model.ContainerRuntimeSpec")).Return("", createErr)

	err := svc.Expose(accessscope.WithUserScope(context.Background(), ownerID, "", ""), containerID, "app", 8080)
	require.ErrorIs(t, err, createErr)
	dockerAPI.AssertNotCalled(t, "RemoveContainer", mock.Anything, "old-docker", true)
}

func TestContainerServiceDeleteIgnoresMissingDockerContainer(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := coremocks.NewContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	svc := NewContainerService(repo, nil, nil, dockerAPI, nil, staticConfig{}, nil, "", slog.Default())

	repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{ID: containerID, OwnerID: ownerID, DockerID: "missing-docker"}, nil)
	dockerAPI.EXPECT().RemoveContainer(mock.Anything, "missing-docker", true).Return(cerrdefs.ErrNotFound)
	repo.EXPECT().Delete(mock.Anything, containerID).Return(nil)
	repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(0, nil)
	dockerAPI.EXPECT().RemoveNetwork(mock.Anything, "net_user_"+ownerID.String()).Return(nil)

	require.NoError(t, svc.Delete(accessscope.WithUserScope(context.Background(), ownerID, "", ""), containerID))
}

func TestContainerServiceStopMarksMissingDockerContainerAndBlocksUse(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := newContainerStateRepoMock(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	cfg := coremocks.NewConfigManager(t)
	svc := NewContainerService(repo, nil, nil, dockerAPI, nil, cfg, nil, "", slog.Default())
	var markedStatus string

	cfg.EXPECT().Get().Return(staticConfig{}.Get()).Maybe()
	repo.ContainerRepository.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{ID: containerID, OwnerID: ownerID, DockerID: "missing-docker", Status: model.ContainerStatusRunning}, nil)
	repo.ContainerStateRepository.EXPECT().CreateOperationAndSetDesired(mock.Anything, containerID, model.ContainerStatusExited, mock.AnythingOfType("model.ResourceOperation")).Return(nil)
	dockerAPI.EXPECT().StopContainer(mock.Anything, "missing-docker", staticConfig{}.Get().ContainerStopTimeout).Return(cerrdefs.ErrNotFound)
	repo.ContainerStateRepository.EXPECT().
		MarkStatusError(mock.Anything, containerID, model.ContainerStatusMissing, mock.Anything).
		Run(func(ctx context.Context, id uuid.UUID, status string, cause error) {
			markedStatus = status
		}).
		Return(nil)
	repo.ContainerStateRepository.EXPECT().CompleteLatestOperation(mock.Anything, model.ResourceTypeContainer, containerID, model.OperationStatusFailed, mock.Anything).Return(nil)

	err := svc.Stop(accessscope.WithUserScope(context.Background(), ownerID, "", ""), containerID)
	require.Error(t, err)
	require.Equal(t, model.ContainerStatusMissing, markedStatus)
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

func TestContainerServiceCreateTimeoutClosesOperationWithDetachedContext(t *testing.T) {
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
	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaRAMMB: 1024, QuotaDiskMB: 1024}, nil)
	repo.ContainerRepository.EXPECT().GetUserReservedMemory(mock.Anything, ownerID).Return(int64(0), nil)
	metrics.EXPECT().GetTotalMemory().Return(int64(8*1024*1024*1024), nil)
	repo.ContainerRepository.EXPECT().GetTotalSystemReservedMemory(mock.Anything).Return(int64(0), nil)
	imageRepo.EXPECT().List(mock.Anything, mock.MatchedBy(func(opts model.ListOptions) bool {
		return opts.OwnerID != nil && *opts.OwnerID == ownerID
	})).Return(nil, 0, nil)
	repo.ContainerCreateRepository.EXPECT().SaveWithMountsAndOperation(mock.Anything, mock.AnythingOfType("model.Container"), mock.Anything, mock.AnythingOfType("model.ResourceOperation"), false, false).Return(nil)
	dockerAPI.EXPECT().EnsureUserNetwork(mock.Anything, "net_user_"+ownerID.String()).Return("network-id", nil)
	dockerAPI.EXPECT().ImageExists(mock.Anything, "nginx:latest").Return(false, nil)
	dockerAPI.EXPECT().PullImage(mock.Anything, "nginx:latest").Run(func(ctx context.Context, imageName string) {
		cancel()
	}).Return(context.DeadlineExceeded)
	repo.ContainerStateRepository.EXPECT().
		MarkStatusError(mock.Anything, mock.AnythingOfType("uuid.UUID"), model.ContainerStatusError, mock.Anything).
		Run(func(ctx context.Context, id uuid.UUID, status string, cause error) {
			require.NoError(t, ctx.Err())
			require.ErrorIs(t, cause, apperrors.ErrTimeout)
		}).
		Return(nil)
	repo.ContainerStateRepository.EXPECT().
		CompleteLatestOperation(mock.Anything, model.ResourceTypeContainer, mock.AnythingOfType("uuid.UUID"), model.OperationStatusFailed, mock.Anything).
		Run(func(ctx context.Context, resourceType string, resourceID uuid.UUID, status string, cause error) {
			require.NoError(t, ctx.Err())
			require.ErrorIs(t, cause, apperrors.ErrTimeout)
		}).
		Return(nil)

	_, err := svc.Create(ctx, model.ContainerCreateParams{
		Name:              "web",
		ImageTag:          "nginx:latest",
		RequestedMemoryMB: 128,
	})
	require.ErrorIs(t, err, apperrors.ErrTimeout)
}

func TestContainerServiceExposePreservesNetworkAlias(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := coremocks.NewContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	cfg := coremocks.NewConfigManager(t)
	svc := NewContainerService(repo, nil, nil, dockerAPI, nil, cfg, nil, "", slog.Default())
	auditor := &recordingAuditRecorder{}
	svc.SetAuditRecorder(auditor)
	var createdParams model.ContainerRuntimeSpec
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
	dockerAPI.EXPECT().InspectContainer(mock.Anything, "old-docker").Return(model.ContainerInspection{
		Name:              "/usr_old",
		Image:             "nginx:latest",
		MemoryLimitBytes:  128,
		MemoryReservation: 128,
		CPUShares:         1024,
		State:             model.ContainerState{Running: false},
	}, nil)
	dockerAPI.EXPECT().CreateContainer(mock.Anything, mock.AnythingOfType("model.ContainerRuntimeSpec")).Run(func(ctx context.Context, params model.ContainerRuntimeSpec) {
		createdParams = params
	}).Return("new-docker", nil)
	repo.EXPECT().UpdateDockerID(mock.Anything, containerID, "new-docker").Return(nil)
	repo.EXPECT().UpdateRouting(mock.Anything, containerID, "app", 8080).Return(nil)
	dockerAPI.EXPECT().RemoveContainer(mock.Anything, "old-docker", true).Return(nil)

	require.NoError(t, svc.Expose(accessscope.WithUserScope(context.Background(), ownerID, "alice", ""), containerID, "app", 8080))
	require.Equal(t, "api", createdParams.NetworkAlias)
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

	repo.EXPECT().GetRunning(mock.Anything).Return([]model.Container{{ID: uuid.New(), DockerID: "docker-id", BaseMemoryReservation: 128}}, nil)
	metrics.EXPECT().GetTotalMemory().Return(int64(1024), nil)
	cfg.EXPECT().Get().Return(staticConfig{}.Get()).Maybe()
	dockerAPI.EXPECT().
		UpdateContainerResources(mock.Anything, "docker-id", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(ctx context.Context, dockerID string, memoryLimit, memoryReservation, cpuShares int64, memorySwapMultiplier float64) {
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
		MaxBurstMultiplier:                   4,
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
		MaxContainersPerUser:                 10,
		MaxVolumesPerUser:                    10,
		ImageBuildsEnabled:                   true,
		GitSourcesEnabled:                    true,
		ComposeDependencyWaitTimeoutMinutes:  1,
		ComposeDependencyPollIntervalSeconds: 1,
	}
}
