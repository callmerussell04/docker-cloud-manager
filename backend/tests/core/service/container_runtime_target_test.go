package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	. "github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestContainerServiceGetRuntimeTargetAllowsOwner(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := coremocks.NewContainerRepository(t)
	svc := NewContainerService(repo, nil, nil, nil, nil, nil, nil, "", nil)

	repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{
		ID:               containerID,
		OwnerID:          ownerID,
		DockerID:         "docker-id",
		Status:           model.ContainerStatusRunning,
		DockerGeneration: 3,
	}, nil)

	got, err := svc.GetRuntimeTarget(accessscope.WithUserScope(context.Background(), ownerID, "alice", "user"), containerID)
	require.NoError(t, err)
	require.Equal(t, containerID, got.ContainerID)
	require.Equal(t, "docker-id", got.DockerID)
	require.Equal(t, ownerID, got.OwnerID)
	require.Equal(t, 3, got.DockerGeneration)
}

func TestContainerServiceGetRuntimeTargetHidesOtherOwners(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := coremocks.NewContainerRepository(t)
	svc := NewContainerService(repo, nil, nil, nil, nil, nil, nil, "", nil)

	repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{
		ID:       containerID,
		OwnerID:  ownerID,
		DockerID: "docker-id",
		Status:   model.ContainerStatusRunning,
	}, nil)

	_, err := svc.GetRuntimeTarget(accessscope.WithUserScope(context.Background(), uuid.New(), "bob", "user"), containerID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
}

func TestContainerServiceGetRuntimeTargetAllowsAdmin(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := coremocks.NewContainerRepository(t)
	svc := NewContainerService(repo, nil, nil, nil, nil, nil, nil, "", nil)

	repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{ID: containerID, OwnerID: ownerID, DockerID: "docker-id", Status: model.ContainerStatusRunning}, nil)

	_, err := svc.GetRuntimeTarget(accessscope.WithAdminScope(context.Background(), uuid.New(), "admin", "admin"), containerID)
	require.NoError(t, err)
}

func TestContainerServiceGetRuntimeTargetRejectsUnavailableContainer(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := coremocks.NewContainerRepository(t)
	svc := NewContainerService(repo, nil, nil, nil, nil, nil, nil, "", nil)

	repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{ID: containerID, OwnerID: ownerID, Status: model.ContainerStatusMissing}, nil).Once()
	_, err := svc.GetRuntimeTarget(accessscope.WithUserScope(context.Background(), ownerID, "alice", "user"), containerID)
	require.ErrorIs(t, err, apperrors.ErrConflict)

	repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{ID: containerID, OwnerID: ownerID, Status: model.ContainerStatusRunning}, nil).Once()
	_, err = svc.GetRuntimeTarget(accessscope.WithUserScope(context.Background(), ownerID, "alice", "user"), containerID)
	require.True(t, errors.Is(err, apperrors.ErrNotFound))
}
