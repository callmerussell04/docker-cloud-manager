package service_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// wantMonitoringReason is the monitoring reason each host admission limit is
// reported as.
var wantMonitoringReason = map[apperrors.Limit]string{
	apperrors.LimitHostMemory: "host_memory_exhausted",
	apperrors.LimitHostCPU:    "host_cpu_exhausted",
	apperrors.LimitDiskFloor:  "host_disk_floor",
}

// Monitoring's admission status comes from the check requests go through: in
// a world where a container create with the default reservations is rejected
// on a host limit, monitoring is blocked with that limit as its reason, and
// where the create is admitted, monitoring is open.
func TestMonitoringAdmissionStatusAgreesWithARequestInTheSameWorld(t *testing.T) {
	for _, tc := range []struct {
		name  string
		world func(*admissionWorld)
		want  apperrors.Limit
	}{
		{name: "fits", world: func(*admissionWorld) {}},
		{
			name:  "host memory",
			world: func(w *admissionWorld) { w.otherMemory = 1400 * admissionMiB },
			want:  apperrors.LimitHostMemory,
		},
		{
			name: "host memory held by active builds",
			world: func(w *admissionWorld) {
				w.otherMemory = 1000 * admissionMiB
				w.activeBuilds = 2
			},
			want: apperrors.LimitHostMemory,
		},
		{
			name:  "host cpu",
			world: func(w *admissionWorld) { w.otherCPU = 1300 },
			want:  apperrors.LimitHostCPU,
		},
		{
			name: "host cpu held by active builds",
			world: func(w *admissionWorld) {
				w.otherCPU = 800
				w.activeBuilds = 2
			},
			want: apperrors.LimitHostCPU,
		},
		{
			name: "builds queued behind the concurrency limit reserve nothing",
			world: func(w *admissionWorld) {
				w.otherMemory = 1000 * admissionMiB
				w.otherCPU = 800
				w.activeBuilds = 6
				w.maxConcurrentBuilds = 1
			},
		},
		{
			name:  "disk floor",
			world: func(w *admissionWorld) { w.diskFreeBytes = 512 * admissionMiB },
			want:  apperrors.LimitDiskFloor,
		},
		{
			name: "nothing is checked against the disk with a zero floor",
			world: func(w *admissionWorld) {
				w.diskFloorBytes = 0
				w.diskFreeBytes = 0
			},
		},
		{
			// The check rejects at the first limit, so the reason is the one
			// a request would be refused with.
			name: "host memory is reported before the disk floor",
			world: func(w *admissionWorld) {
				w.otherMemory = 1400 * admissionMiB
				w.diskFreeBytes = 0
			},
			want: apperrors.LimitHostMemory,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := roomyAdmissionWorld()
			tc.world(&w)

			_, requestErr := admitContainerCreate(t, w)
			stats, err := monitoringInWorld(t, w).GetSystemMonitoring(t.Context())
			require.NoError(t, err)

			if tc.want == "" {
				require.NoError(t, requestErr)
				require.Equal(t, "open", stats.AdmissionStatus)
				require.Empty(t, stats.AdmissionReasons)
				return
			}
			require.Equal(t, tc.want, apperrors.LimitOf(requestErr), "the request is rejected on the limit")
			require.Equal(t, "blocked", stats.AdmissionStatus)
			require.Equal(t, []string{wantMonitoringReason[tc.want]}, stats.AdmissionReasons, "monitoring reports the limit the request met")
		})
	}
}

// monitoringInWorld is a stats service whose host, reservations and builds
// report w: its container reservations are read from the same store an
// admission request in w meets.
func monitoringInWorld(t *testing.T, w admissionWorld) *service.StatsService {
	t.Helper()
	store := admissionStore(t, w, uuid.New())
	containers := coremocks.NewStatsContainerRepository(t)
	containers.EXPECT().GetTotalSystemReservedMemory(mock.Anything).RunAndReturn(store.GetTotalSystemReservedMemory)
	containers.EXPECT().GetTotalSystemReservedCPU(mock.Anything).RunAndReturn(store.GetTotalSystemReservedCPU)
	containers.EXPECT().GetSystemStatusCounts(mock.Anything).Return(model.ContainerStatusCounts{}, nil)
	volumes := coremocks.NewStatsVolumeRepository(t)
	volumes.EXPECT().GetTotalUsedVolumeBytes(mock.Anything).Return(0, nil)
	volumes.EXPECT().GetTotalVolumeSizeBytes(mock.Anything).Return(0, nil)
	volumes.EXPECT().GetUnusedVolumePromiseBytes(mock.Anything).Return(0, nil)
	volumes.EXPECT().CountAll(mock.Anything).Return(0, nil)
	images := coremocks.NewStatsImageRepository(t)
	images.EXPECT().GetTotalImageBytes(mock.Anything).Return(0, nil)
	images.EXPECT().CountAll(mock.Anything).Return(0, nil)
	builds := coremocks.NewStatsBuildRepository(t)
	builds.EXPECT().CountActive(mock.Anything).Return(w.activeBuilds, nil)
	builds.EXPECT().CountAll(mock.Anything).Return(w.activeBuilds, nil)
	projects := coremocks.NewStatsProjectRepository(t)
	projects.EXPECT().CountAll(mock.Anything).Return(0, nil)
	metrics := coremocks.NewStatsMetricsProvider(t)
	metrics.EXPECT().GetCPULoad().Return(0, nil)
	metrics.EXPECT().GetMemoryStats().Return(model.HostMemoryStats{TotalBytes: admissionHostMemoryBytes}, nil)
	metrics.EXPECT().GetLogicalCPUs().Return(admissionHostCPUs, nil)
	metrics.EXPECT().GetDiskUsage(mock.Anything).Return(model.HostDiskStats{FreeBytes: w.diskFreeBytes}, nil)
	borrowed := coremocks.NewStatsBorrowedMemory(t)
	borrowed.EXPECT().BorrowedMemoryBytes().Return(0)
	rootfsQuota := coremocks.NewStatsRootfsQuota(t)
	rootfsQuota.EXPECT().RootfsQuotaStatus().Return(model.RootfsQuotaEnforced)
	volumeSize := coremocks.NewStatsVolumeSizeMode(t)
	volumeSize.EXPECT().VolumeSizeMode().Return(model.VolumeSizeMeasured)
	cfg := coretest.StaticConfig{Config: w.config()}
	svc, err := service.NewStatsService(service.StatsServiceDeps{
		Containers: containers, Volumes: volumes, Images: images, Builds: builds, Projects: projects,
		Metrics: metrics, Config: cfg, Quotas: w.quotas(t, uuid.New(), cfg), Borrowed: borrowed,
		RootfsQuota: rootfsQuota, VolumeSizeMode: volumeSize,
	})
	require.NoError(t, err)
	return svc
}
