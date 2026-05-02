package service

import (
	"context"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

func TestContainerServiceGetRuntimeTargetAllowsOwner(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	svc := &ContainerService{repo: &containerCleanupRepoFake{
		container: model.Container{
			ID:               containerID,
			OwnerID:          ownerID,
			DockerID:         "docker-id",
			Status:           model.ContainerStatusRunning,
			DockerGeneration: 3,
		},
	}}

	got, err := svc.GetRuntimeTarget(accessscope.WithUserScope(context.Background(), ownerID, "alice", "user"), containerID)
	if err != nil {
		t.Fatalf("GetRuntimeTarget() error = %v", err)
	}
	if got.ContainerID != containerID || got.DockerID != "docker-id" || got.OwnerID != ownerID || got.DockerGeneration != 3 {
		t.Fatalf("runtime target mismatch: %+v", got)
	}
}

func TestContainerServiceGetRuntimeTargetHidesOtherOwners(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	svc := &ContainerService{repo: &containerCleanupRepoFake{
		container: model.Container{
			ID:       containerID,
			OwnerID:  ownerID,
			DockerID: "docker-id",
			Status:   model.ContainerStatusRunning,
		},
	}}

	_, err := svc.GetRuntimeTarget(accessscope.WithUserScope(context.Background(), uuid.New(), "bob", "user"), containerID)
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("GetRuntimeTarget() error = %v, want ErrNotFound", err)
	}
}

func TestContainerServiceGetRuntimeTargetAllowsAdmin(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	svc := &ContainerService{repo: &containerCleanupRepoFake{
		container: model.Container{
			ID:       containerID,
			OwnerID:  ownerID,
			DockerID: "docker-id",
			Status:   model.ContainerStatusRunning,
		},
	}}

	if _, err := svc.GetRuntimeTarget(accessscope.WithAdminScope(context.Background(), uuid.New(), "admin", "admin"), containerID); err != nil {
		t.Fatalf("GetRuntimeTarget() error = %v", err)
	}
}

func TestContainerServiceGetRuntimeTargetRejectsUnavailableContainer(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	svc := &ContainerService{repo: &containerCleanupRepoFake{
		container: model.Container{
			ID:      containerID,
			OwnerID: ownerID,
			Status:  model.ContainerStatusMissing,
		},
	}}

	_, err := svc.GetRuntimeTarget(accessscope.WithUserScope(context.Background(), ownerID, "alice", "user"), containerID)
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("GetRuntimeTarget() error = %v, want ErrConflict", err)
	}

	svc.repo = &containerCleanupRepoFake{container: model.Container{
		ID:      containerID,
		OwnerID: ownerID,
		Status:  model.ContainerStatusRunning,
	}}
	_, err = svc.GetRuntimeTarget(accessscope.WithUserScope(context.Background(), ownerID, "alice", "user"), containerID)
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("GetRuntimeTarget() empty docker id error = %v, want ErrNotFound", err)
	}
}
