package compose_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	composemocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service/compose"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestReleaseComposeSourceDeletesObjectAndReleasesReservation(t *testing.T) {
	for name, releaseErr := range map[string]error{
		"released":            nil,
		"reservation_missing": apperrors.ErrNotFound,
	} {
		t.Run(name, func(t *testing.T) {
			const objectKey = "compose-sources/source.zip"
			store := composemocks.NewObjectStorage(t)
			store.EXPECT().DeleteObject(mock.Anything, objectKey).Return(nil).Once()
			staged := composemocks.NewStagedObjectRepository(t)
			staged.EXPECT().Release(mock.Anything, objectKey).Return(releaseErr).Once()
			orch, err := compose.NewOrchestrator(compose.OrchestratorDeps{
				Projects:          composemocks.NewProjectRepository(t),
				DeploymentCounter: composemocks.NewActiveComposeDeploymentCounter(t),
				Capacity:          composemocks.NewCapacityChecker(t),
				Config:            coretest.StaticConfig{Config: composeTestConfig()},
				ObjectStore:       store,
				StagedObjects:     staged,
				CapacityLock:      composemocks.NewCapacityLock(t),
				ReservedDisk:      composemocks.NewReservedDiskReader(t),
				Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
			})
			require.NoError(t, err)

			orch.ReleaseComposeSource(context.Background(), objectKey)
		})
	}
}
