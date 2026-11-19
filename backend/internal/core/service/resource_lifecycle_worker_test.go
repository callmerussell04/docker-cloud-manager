package service_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// A volume or image operation runs under the container operations' handler
// deadline, so the operation recovery pass can tell when no worker is still
// acting on it.
func TestResourceLifecycleWorkerRunsEachOperationUnderTheHandlerDeadline(t *testing.T) {
	for _, resourceType := range []string{model.ResourceTypeVolume, model.ResourceTypeImage} {
		t.Run(resourceType, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cfg := coretest.SystemConfig()
				cfg.ContainerCreateTimeoutMinutes = 7
				var deadline time.Time
				var hasDeadline bool
				record := func(ctx context.Context, _, _ uuid.UUID) error {
					deadline, hasDeadline = ctx.Deadline()
					return nil
				}
				volumes := coremocks.NewResourceVolumeExecutor(t)
				images := coremocks.NewResourceImageExecutor(t)
				if resourceType == model.ResourceTypeVolume {
					volumes.EXPECT().ExecuteQueuedVolumeOperation(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(record).Once()
				} else {
					images.EXPECT().ExecuteQueuedImageOperation(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(record).Once()
				}
				worker := service.NewResourceLifecycleWorker(volumes, images, coretest.StaticConfig{Config: cfg}, coretest.DiscardLogger())

				err := worker.HandleMessage(context.Background(), queue.ResourceLifecycleMessage{
					OperationID:  uuid.NewString(),
					ResourceType: resourceType,
					ResourceID:   uuid.NewString(),
				})

				require.NoError(t, err)
				require.True(t, hasDeadline)
				require.Equal(t, time.Now().Add(7*time.Minute), deadline)
			})
		})
	}
}
