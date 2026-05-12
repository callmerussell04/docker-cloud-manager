package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	. "github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestBuildOutboxWorkerPublishesAndMarksTerminalOrMissingDiscarded(t *testing.T) {
	pendingID := uuid.New()
	terminalID := uuid.New()
	missingID := uuid.New()
	publishID := uuid.New()
	terminalOutboxID := uuid.New()
	missingOutboxID := uuid.New()
	repo := coremocks.NewBuildOutboxRepository(t)
	publisher := coremocks.NewBuildQueuePublisher(t)
	var published []uuid.UUID
	var discarded []uuid.UUID

	repo.EXPECT().LeasePendingBuildOutbox(mock.Anything, 100).Return([]model.BuildQueueOutbox{
		{ID: publishID, BuildID: pendingID, Exchange: "ex", RoutingKey: "rk", Payload: []byte("ok")},
		{ID: terminalOutboxID, BuildID: terminalID},
		{ID: missingOutboxID, BuildID: missingID},
	}, nil)
	repo.EXPECT().GetByID(mock.Anything, pendingID).Return(model.Build{ID: pendingID, Status: model.BuildStatusPending}, nil)
	publisher.EXPECT().Publish(mock.Anything, "ex", "rk", []byte("ok")).Return(nil)
	repo.EXPECT().MarkBuildOutboxPublished(mock.Anything, publishID).Run(func(ctx context.Context, id uuid.UUID) {
		published = append(published, id)
	}).Return(nil)
	repo.EXPECT().GetByID(mock.Anything, terminalID).Return(model.Build{ID: terminalID, Status: model.BuildStatusCanceled}, nil)
	repo.EXPECT().MarkBuildOutboxDiscarded(mock.Anything, terminalOutboxID, nil).Return(nil)
	repo.EXPECT().GetByID(mock.Anything, missingID).Return(model.Build{}, apperrors.ErrNotFound)
	repo.EXPECT().MarkBuildOutboxDiscarded(mock.Anything, missingOutboxID, mock.Anything).Run(func(ctx context.Context, id uuid.UUID, cause error) {
		discarded = append(discarded, id)
	}).Return(nil)

	runBuildOutboxOnce(repo, publisher, newBuildOutboxConfigMock(t))

	require.Equal(t, []uuid.UUID{publishID}, published)
	require.Equal(t, []uuid.UUID{missingOutboxID}, discarded)
}

func TestBuildOutboxWorkerReturnsPendingOnFetchOrPublishError(t *testing.T) {
	fetchBuildID := uuid.New()
	publishBuildID := uuid.New()
	fetchOutboxID := uuid.New()
	publishOutboxID := uuid.New()
	fetchErr := errors.New("db down")
	publishErr := errors.New("broker down")
	repo := coremocks.NewBuildOutboxRepository(t)
	publisher := coremocks.NewBuildQueuePublisher(t)
	var pending []uuid.UUID

	repo.EXPECT().LeasePendingBuildOutbox(mock.Anything, 100).Return([]model.BuildQueueOutbox{
		{ID: fetchOutboxID, BuildID: fetchBuildID},
		{ID: publishOutboxID, BuildID: publishBuildID},
	}, nil)
	repo.EXPECT().GetByID(mock.Anything, fetchBuildID).Return(model.Build{}, fetchErr)
	repo.EXPECT().MarkBuildOutboxPending(mock.Anything, fetchOutboxID, fetchErr).Run(func(ctx context.Context, id uuid.UUID, cause error) {
		pending = append(pending, id)
	}).Return(nil)
	repo.EXPECT().GetByID(mock.Anything, publishBuildID).Return(model.Build{ID: publishBuildID, Status: model.BuildStatusPending}, nil)
	publisher.EXPECT().Publish(mock.Anything, "", "", []byte(nil)).Return(publishErr)
	repo.EXPECT().MarkBuildOutboxPending(mock.Anything, publishOutboxID, publishErr).Run(func(ctx context.Context, id uuid.UUID, cause error) {
		pending = append(pending, id)
	}).Return(nil)

	runBuildOutboxOnce(repo, publisher, newBuildOutboxConfigMock(t))

	require.ElementsMatch(t, []uuid.UUID{fetchOutboxID, publishOutboxID}, pending)
}

func TestComposeOutboxWorkerPublishesAndDiscardsTerminalJobs(t *testing.T) {
	pendingJobID := uuid.New()
	terminalJobID := uuid.New()
	publishOutboxID := uuid.New()
	discardOutboxID := uuid.New()
	repo := coremocks.NewComposeOutboxRepository(t)
	publisher := coremocks.NewBuildQueuePublisher(t)
	var published []uuid.UUID
	var discarded []uuid.UUID

	repo.EXPECT().LeasePendingComposeOutbox(mock.Anything, 100).Return([]model.ComposeDeploymentOutbox{
		{ID: publishOutboxID, JobID: pendingJobID, Exchange: "ex", RoutingKey: "rk", Payload: []byte("ok")},
		{ID: discardOutboxID, JobID: terminalJobID},
	}, nil)
	repo.EXPECT().GetComposeDeploymentJob(mock.Anything, pendingJobID).Return(model.ComposeDeploymentJob{ID: pendingJobID, Status: model.ComposeDeploymentStatusRunning}, nil)
	publisher.EXPECT().Publish(mock.Anything, "ex", "rk", []byte("ok")).Return(nil)
	repo.EXPECT().MarkComposeOutboxPublished(mock.Anything, publishOutboxID).Run(func(ctx context.Context, id uuid.UUID) {
		published = append(published, id)
	}).Return(nil)
	repo.EXPECT().GetComposeDeploymentJob(mock.Anything, terminalJobID).Return(model.ComposeDeploymentJob{ID: terminalJobID, Status: model.ComposeDeploymentStatusFailed}, nil)
	repo.EXPECT().MarkComposeOutboxDiscarded(mock.Anything, discardOutboxID, nil).Return(nil)

	runComposeOutboxOnce(repo, publisher, newComposeOutboxConfigMock(t))

	require.Equal(t, []uuid.UUID{publishOutboxID}, published)
	require.Empty(t, discarded)
}

func runBuildOutboxOnce(repo *coremocks.BuildOutboxRepository, publisher *coremocks.BuildQueuePublisher, cfg *coremocks.BuildOutboxConfigProvider) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	NewBuildOutboxWorker(repo, publisher, cfg, discardLogger()).Run(ctx)
}

func runComposeOutboxOnce(repo *coremocks.ComposeOutboxRepository, publisher *coremocks.BuildQueuePublisher, cfg *coremocks.ComposeOutboxConfigProvider) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	NewComposeOutboxWorker(repo, publisher, cfg, discardLogger()).Run(ctx)
}

func newBuildOutboxConfigMock(t *testing.T) *coremocks.BuildOutboxConfigProvider {
	t.Helper()
	cfg := coremocks.NewBuildOutboxConfigProvider(t)
	cfg.EXPECT().Get().Return(config.SystemConfig{
		BuildOutboxBatchSize:       100,
		BuildOutboxIntervalSeconds: 1,
	}).Maybe()
	return cfg
}

func newComposeOutboxConfigMock(t *testing.T) *coremocks.ComposeOutboxConfigProvider {
	t.Helper()
	cfg := coremocks.NewComposeOutboxConfigProvider(t)
	cfg.EXPECT().Get().Return(config.SystemConfig{
		ComposeOutboxBatchSize:       100,
		ComposeOutboxIntervalSeconds: 1,
	}).Maybe()
	return cfg
}
