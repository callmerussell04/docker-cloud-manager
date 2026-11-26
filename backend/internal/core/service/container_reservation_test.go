package service_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A container reserves the memory and CPU its owner chose at create, or the
// default reservations of the runtime settings when the owner gave none.
func TestContainerCreateRecordsTheChosenReservation(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		memoryMB, millicores int64
		wantMemory, wantCPU  int64
	}{
		{name: "no figures take the defaults", wantMemory: 256 * admissionMiB, wantCPU: 250},
		{name: "chosen figures", memoryMB: 128, millicores: 100, wantMemory: 128 * admissionMiB, wantCPU: 100},
		{name: "memory only", memoryMB: 64, wantMemory: 64 * admissionMiB, wantCPU: 250},
		{name: "cpu only", millicores: 400, wantMemory: 256 * admissionMiB, wantCPU: 400},
		{name: "the smallest figures", memoryMB: 16, millicores: 10, wantMemory: 16 * admissionMiB, wantCPU: 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := roomyAdmissionWorld()
			ownerID := uuid.New()
			store := admissionStore(t, w, ownerID)
			svc := admissionContainerService(t, w, ownerID, store, coretest.StaticConfig{Config: w.config()})
			ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")

			id, err := svc.Create(ctx, model.ContainerCreateParams{
				Name: "web", ImageTag: "nginx:latest",
				MemoryReservationMB: tc.memoryMB, CPUReservationMillicores: tc.millicores,
			})

			require.NoError(t, err)
			got, err := store.GetByID(context.Background(), id)
			require.NoError(t, err)
			require.Equal(t, tc.wantMemory, got.BaseMemoryReservation)
			require.Equal(t, tc.wantCPU, got.BaseCPUReservation)
		})
	}
}

// A reservation outside the bounds is a bad request decided before the owner
// capacity lock, and its message does not echo the figure. The strict mocks
// of validContainerServiceDeps fail the test on any repository call.
func TestContainerCreateRejectsAReservationOutsideTheBounds(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		memoryMB, millicores int64
	}{
		{name: "negative memory", memoryMB: -64},
		{name: "memory below 16 MiB", memoryMB: 15},
		{name: "memory above 1 TiB", memoryMB: 1<<20 + 1},
		{name: "memory that would overflow bytes", memoryMB: 1 << 50},
		{name: "negative cpu", millicores: -250},
		{name: "cpu below 10 millicores", millicores: 9},
		{name: "cpu above the largest cpu quota", millicores: 1_000_000_001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := validContainerServiceDeps(t)
			deps.Config = coretest.StaticConfig{Config: containerCreateConfig()}
			svc := newTestContainerService(t, deps)
			ctx := accessscope.WithUserScope(context.Background(), uuid.New(), "", "")

			_, err := svc.Create(ctx, model.ContainerCreateParams{
				Name: "web", ImageTag: "nginx:latest",
				MemoryReservationMB: tc.memoryMB, CPUReservationMillicores: tc.millicores,
			})

			require.ErrorIs(t, err, apperrors.ErrBadRequest)
			message := apperrors.SafeMessage(err)
			for _, figure := range []int64{tc.memoryMB, tc.millicores} {
				if figure != 0 {
					require.NotContains(t, message, strconv.FormatInt(figure, 10), "the message does not echo the figure")
				}
			}
		})
	}
}

// A reservation within the bounds but above what the owner's quota leaves is
// the user memory or CPU verdict, not a bad request.
func TestContainerCreateAboveTheQuotaIsTheQuotaVerdict(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		memoryMB, millicores int64
		want                 apperrors.Limit
	}{
		{name: "memory", memoryMB: 1 << 20, want: apperrors.LimitUserMemory},
		{name: "cpu", millicores: 1_000_000_000, want: apperrors.LimitUserCPU},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := roomyAdmissionWorld()
			ownerID := uuid.New()
			store := admissionStore(t, w, ownerID)
			svc := admissionContainerService(t, w, ownerID, store, coretest.StaticConfig{Config: w.config()})
			ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")

			_, err := svc.Create(ctx, model.ContainerCreateParams{
				Name: "web", ImageTag: "nginx:latest",
				MemoryReservationMB: tc.memoryMB, CPUReservationMillicores: tc.millicores,
			})

			require.ErrorIs(t, err, apperrors.ErrQuotaExceeded)
			require.Equal(t, tc.want, apperrors.LimitOf(err))
			require.Zero(t, queuedCount(t, store))
		})
	}
}
