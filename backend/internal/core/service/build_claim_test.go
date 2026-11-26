package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// The cap a claim gets is max_concurrent_builds, or fewer when the free disk
// above the floor cannot hold that many build slots of build_max_disk_bytes
// each. The existing atomic claim enforces it, so a build over it stays
// pending and Builder claims it again.
func TestBuildServiceClaimCapFollowsTheDiskAboveTheFloor(t *testing.T) {
	const gib = int64(1 << 30)
	for _, tc := range []struct {
		name      string
		freeBytes int64
		floor     int64
		maxDisk   int64
		wantCap   int
	}{
		{name: "the disk holds every slot", freeBytes: 10 * gib, floor: gib, maxDisk: gib, wantCap: 4},
		{name: "the disk holds two slots", freeBytes: 3*gib + gib/2, floor: gib, maxDisk: gib, wantCap: 2},
		{name: "the disk holds exactly three slots", freeBytes: 4 * gib, floor: gib, maxDisk: gib, wantCap: 3},
		{name: "the disk holds no slot", freeBytes: 2*gib - 1, floor: gib, maxDisk: gib, wantCap: 0},
		{name: "the disk is below the floor", freeBytes: gib / 2, floor: gib, maxDisk: gib, wantCap: 0},
		{name: "a zero floor reads no disk", freeBytes: -1, floor: 0, maxDisk: gib, wantCap: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := coretest.IntegrationConfig()
			cfg.MaxConcurrentBuilds = 4
			cfg.HostMinFreeDiskBytes = tc.floor
			cfg.BuildMaxDiskBytes = tc.maxDisk
			disk := coremocks.NewHostDiskMetricsProvider(t)
			if tc.freeBytes >= 0 {
				disk.EXPECT().GetDiskUsage(mock.Anything).Return(model.HostDiskStats{FreeBytes: tc.freeBytes}, nil).Once()
			}
			auth := coremocks.NewBuildRegistryAuthorizer(t)
			buildID := uuid.New()
			pending := model.Build{ID: buildID, Status: model.BuildStatusPending}
			auth.EXPECT().ClaimBuild(mock.Anything, buildID, mock.Anything, tc.wantCap).Return(pending, false, nil).Once()
			svc := newTestBuildService(t, service.BuildServiceDeps{
				Repository:   newBuildRepository(t),
				Images:       coremocks.NewBuildImageRepository(t),
				Registry:     coremocks.NewImageRegistryAPI(t),
				Config:       coretest.StaticConfig{Config: cfg},
				DiskMetrics:  disk,
				RegistryAuth: auth,
			})

			b, started, err := svc.StartBuildRecord(context.Background(), buildID)

			require.NoError(t, err)
			require.False(t, started)
			require.Equal(t, model.BuildStatusPending, b.Status)
		})
	}
}

// A failed disk read refuses the claim as unclaimed: the claim runs with a cap
// of zero, so a pending build stays pending and a build that is no longer
// pending still returns its persisted status. It is never an error to
// Builder, because a claim error can dead-letter the build.
func TestBuildServiceClaimIsRefusedWhenTheDiskCannotBeRead(t *testing.T) {
	cfg := coretest.IntegrationConfig()
	cfg.MaxConcurrentBuilds = 4
	cfg.HostMinFreeDiskBytes = 1 << 30
	disk := coremocks.NewHostDiskMetricsProvider(t)
	disk.EXPECT().GetDiskUsage(mock.Anything).Return(model.HostDiskStats{}, errors.New("statfs failed")).Once()
	auth := coremocks.NewBuildRegistryAuthorizer(t)
	buildID := uuid.New()
	auth.EXPECT().ClaimBuild(mock.Anything, buildID, mock.Anything, 0).
		Return(model.Build{ID: buildID, Status: model.BuildStatusPending}, false, nil).Once()
	svc := newTestBuildService(t, service.BuildServiceDeps{
		Repository:   newBuildRepository(t),
		Images:       coremocks.NewBuildImageRepository(t),
		Registry:     coremocks.NewImageRegistryAPI(t),
		Config:       coretest.StaticConfig{Config: cfg},
		DiskMetrics:  disk,
		RegistryAuth: auth,
	})

	b, started, err := svc.StartBuildRecord(context.Background(), buildID)

	require.NoError(t, err)
	require.False(t, started)
	require.Equal(t, model.BuildStatusPending, b.Status)
}
