package docker_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDockerAdapterNetworkContainerAndVolumeSmoke(t *testing.T) {
	if os.Getenv("CORE_TEST_DOCKER") != "1" {
		t.Skip("set CORE_TEST_DOCKER=1 to run Core Docker smoke test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	adapter, err := docker.NewAdapter()
	require.NoError(t, err)
	t.Cleanup(func() { _ = adapter.Close() })

	ownerID := uuid.New()
	containerID := uuid.New()
	volumeID := uuid.New()
	networkName := "dcm_core_test_" + ownerID.String()[:8]
	imageName := getenv("CORE_TEST_DOCKER_IMAGE", "busybox:latest")
	require.NoError(t, adapter.PullImage(ctx, imageName))

	networkID, err := adapter.EnsureUserNetwork(ctx, networkName)
	require.NoError(t, err)
	require.NotEmpty(t, networkID)
	networkIDAgain, err := adapter.EnsureUserNetwork(ctx, networkName)
	require.NoError(t, err)
	require.Equal(t, networkID, networkIDAgain)
	t.Cleanup(func() { _ = adapter.RemoveNetwork(context.Background(), networkName) })

	volumeName, err := adapter.CreateVolume(ctx, model.VolumeRuntimeSpec{
		VolumeName: "dcm_core_test_" + volumeID.String()[:8],
		VolumeID:   volumeID.String(),
		OwnerID:    ownerID.String(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = adapter.RemoveVolume(context.Background(), volumeName, true) })

	volumeInspection, err := adapter.InspectVolume(ctx, volumeName)
	require.NoError(t, err)
	require.Equal(t, volumeName, volumeInspection.Name)
	require.Equal(t, volumeID.String(), volumeInspection.Labels["dcm.volume_id"])
	require.Equal(t, ownerID.String(), volumeInspection.Labels["dcm.owner_id"])

	dockerID, err := adapter.CreateContainer(ctx, model.ContainerRuntimeSpec{
		ContainerID:          containerID.String(),
		OwnerID:              ownerID.String(),
		ContainerName:        "dcm_core_test_" + containerID.String()[:8],
		ImageName:            imageName,
		NetworkName:          networkName,
		Command:              []string{"sh", "-c", "sleep 30"},
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
	t.Cleanup(func() { _ = adapter.RemoveContainer(context.Background(), dockerID, true) })

	require.NoError(t, adapter.StartContainer(ctx, dockerID))
	inspection, err := adapter.InspectContainer(ctx, dockerID)
	require.NoError(t, err)
	require.True(t, inspection.State.Running)
	require.Equal(t, imageName, inspection.Image)
	require.Contains(t, mountTargets(inspection.Mounts), "/data")

	stats, err := adapter.GetContainerStats(ctx, dockerID)
	require.NoError(t, err)
	require.NotZero(t, stats.MemoryLimitBytes)
	require.NoError(t, adapter.StopContainer(ctx, dockerID, 1))
	require.NoError(t, adapter.RemoveContainer(ctx, dockerID, true))
	require.NoError(t, adapter.RemoveVolume(ctx, volumeName, true))
	require.NoError(t, adapter.RemoveNetwork(ctx, networkName))
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func mountTargets(mounts []model.ContainerMountSpec) []string {
	targets := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		targets = append(targets, mount.Target)
	}
	return targets
}
