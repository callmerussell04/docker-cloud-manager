package docker_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/testenv"
)

// Exit codes of the writer container: the write within the size failed, or
// the write past it succeeded. A refused write past the size exits 4.
const (
	quotaSmokeSmallWriteFailed = 3
	quotaSmokeWritePastRefused = 4
)

// Real kernel enforcement of a volume size. It runs only against a Docker host
// whose data directory supports project quotas (for example XFS mounted with
// pquota): set DCM_TEST_VOLUME_QUOTA_DOCKER=1 there. Everywhere else,
// including CI and make test, it skips. docs/deployment-single-vm.md carries
// the same check as a manual step.
func TestDockerAdapterEnforcesVolumeSizeSmoke(t *testing.T) {
	testenv.Enabled(t, "DCM_TEST_VOLUME_QUOTA_DOCKER")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	adapter, err := docker.NewAdapter()
	require.NoError(t, err)
	t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable

	supported, err := adapter.CheckVolumeSizeSupport(ctx)
	require.NoError(t, err)
	require.True(t, supported, "the host check must pass on a host with project quotas")

	ownerID, volumeID, containerID := uuid.New(), uuid.New(), uuid.New()
	imageName := getenv("CORE_TEST_DOCKER_IMAGE", "busybox:latest")
	require.NoError(t, adapter.PullImage(ctx, imageName))
	networkName := "dcm_core_test_" + ownerID.String()[:8]
	_, err = adapter.EnsureUserNetwork(ctx, networkName)
	require.NoError(t, err)
	t.Cleanup(func() { _ = adapter.RemoveNetwork(context.Background(), networkName) }) // best-effort teardown

	volumeName, err := adapter.CreateVolume(ctx, model.VolumeRuntimeSpec{
		VolumeName: "dcm_core_test_" + volumeID.String()[:8],
		VolumeID:   volumeID.String(),
		OwnerID:    ownerID.String(),
		SizeBytes:  16 << 20,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = adapter.RemoveVolume(context.Background(), volumeName, true) }) // best-effort teardown

	// 4 MiB fits the 16 MiB size; 32 MiB more does not.
	dockerID, err := adapter.CreateContainer(ctx, model.ContainerRuntimeSpec{
		ContainerID:   containerID.String(),
		OwnerID:       ownerID.String(),
		ContainerName: "dcm_core_test_" + containerID.String()[:8],
		ImageName:     imageName,
		NetworkName:   networkName,
		Command: []string{"sh", "-c",
			"dd if=/dev/zero of=/data/small bs=1048576 count=4 || exit 3; " +
				"dd if=/dev/zero of=/data/big bs=1048576 count=32 && exit 0; exit 4"},
		MemoryLimitBytes:     64 << 20,
		MemoryReservation:    32 << 20,
		MemorySwapMultiplier: 2,
		CPUShares:            128,
		PidsLimit:            64,
		MaxLogSize:           "1m",
		MaxLogFiles:          "1",
		Generation:           1,
		VolumeMounts:         []model.ContainerMountSpec{{VolumeName: volumeName, Target: "/data"}},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = adapter.RemoveContainer(context.Background(), dockerID, true) }) // best-effort teardown
	require.NoError(t, adapter.StartContainer(ctx, dockerID))

	var state model.ContainerState
	require.Eventually(t, func() bool {
		inspection, err := adapter.InspectContainer(ctx, dockerID)
		if err != nil {
			return false
		}
		state = inspection.State
		return !state.Running && state.Status == "exited"
	}, time.Minute, 250*time.Millisecond)
	require.NotEqual(t, quotaSmokeSmallWriteFailed, state.ExitCode, "a write within the size succeeds")
	require.Equal(t, quotaSmokeWritePastRefused, state.ExitCode, "a write past the size fails inside the container")
}
