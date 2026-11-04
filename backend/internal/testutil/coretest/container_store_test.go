package coretest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestContainerStoreFakeContract(t *testing.T) {
	coretest.RunContainerStoreContract(t, func(*testing.T) coretest.ContainerStore {
		return coretest.NewContainerStoreFake()
	})
}

var errInjected = errors.New("injected")

func TestContainerStoreFakeFailNextFailsThatManyCallsWithoutWriting(t *testing.T) {
	ctx := context.Background()
	store := coretest.NewContainerStoreFake()
	c := model.Container{ID: uuid.New(), OwnerID: uuid.New(), Status: model.ContainerStatusRunning}
	require.NoError(t, store.Save(ctx, c))

	store.SetFaults(coretest.ContainerStoreFaults{
		UpdateStatusByContainerIDAndGeneration: coretest.FailNext(2, errInjected),
	})
	for range 2 {
		require.ErrorIs(t, store.UpdateStatusByContainerIDAndGeneration(ctx, c.ID, 1, model.ContainerStatusExited), errInjected)
		got, err := store.GetByID(ctx, c.ID)
		require.NoError(t, err)
		require.Equal(t, model.ContainerStatusRunning, got.Status)
	}
	require.NoError(t, store.UpdateStatusByContainerIDAndGeneration(ctx, c.ID, 1, model.ContainerStatusExited))
	got, err := store.GetByID(ctx, c.ID)
	require.NoError(t, err)
	require.Equal(t, model.ContainerStatusExited, got.Status)
}

func TestContainerStoreFakeFailAlwaysFailsUntilFaultsAreCleared(t *testing.T) {
	ctx := context.Background()
	store := coretest.NewContainerStoreFake()
	c := model.Container{ID: uuid.New(), OwnerID: uuid.New(), Status: model.ContainerStatusRunning}
	require.NoError(t, store.Save(ctx, c))

	store.SetFaults(coretest.ContainerStoreFaults{GetByID: coretest.FailAlways(errInjected)})
	for range 3 {
		_, err := store.GetByID(ctx, c.ID)
		require.ErrorIs(t, err, errInjected)
	}
	// Other methods are unaffected.
	require.NoError(t, store.UpdateStatusByContainerIDAndGeneration(ctx, c.ID, 1, model.ContainerStatusExited))

	store.SetFaults(coretest.ContainerStoreFaults{})
	got, err := store.GetByID(ctx, c.ID)
	require.NoError(t, err)
	require.Equal(t, model.ContainerStatusExited, got.Status)
}

func TestContainerStoreFakeFailedLockAcquisitionHoldsNoLock(t *testing.T) {
	ctx := context.Background()
	store := coretest.NewContainerStoreFake()
	store.SetFaults(coretest.ContainerStoreFaults{AcquireOwnerCapacityLock: coretest.FailNext(1, errInjected)})

	_, err := store.AcquireOwnerCapacityLock(ctx, uuid.New())
	require.ErrorIs(t, err, errInjected)
	release, err := store.AcquireOwnerCapacityLock(ctx, uuid.New())
	require.NoError(t, err)
	release()
}
