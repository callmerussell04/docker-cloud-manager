package compose_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	compose "github.com/callmerussell04/docker-cloud-manager/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	composemocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service/compose"
	"github.com/stretchr/testify/mock"
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
			orch := compose.NewOrchestrator(context.Background(), composemocks.NewProjectRepository(t), nil, nil, nil,
				composemocks.NewConfigProvider(t), store, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
			orch.SetStagedObjectRepository(staged)

			orch.ReleaseComposeSource(context.Background(), objectKey)
		})
	}
}
