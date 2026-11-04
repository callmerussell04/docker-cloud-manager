package docker_test

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	builderdocker "github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/testenv"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAdapterCleanupOrphanBuildContainersSmoke(t *testing.T) {
	testenv.Enabled(t, "BUILDER_TEST_DOCKER")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	imageName := getenv("BUILDER_TEST_DOCKER_IMAGE", "busybox:latest")
	// A unique owner label keeps the cleanup away from build containers of
	// tests running in parallel, such as the Registry isolation probe.
	managedBy := "dcm-smoke-test-" + uuid.NewString()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })
	pull, err := cli.ImagePull(ctx, imageName, image.PullOptions{})
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, pull)
	require.NoError(t, err)
	require.NoError(t, pull.Close())
	created, err := cli.ContainerCreate(ctx, &container.Config{
		Image: imageName,
		Cmd:   []string{"sh", "-c", "sleep 300"},
		Labels: map[string]string{
			"managed_by":        managedBy,
			"dcm.resource_type": "build",
			"dcm.build_id":      "builder-smoke-test",
		},
	}, nil, nil, nil, "")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = cli.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true})
	})

	adapter, err := builderdocker.NewAdapter(builderdocker.Config{ManagedBy: managedBy})
	require.NoError(t, err)
	t.Cleanup(func() { _ = adapter.Close() })
	require.NoError(t, adapter.CleanupOrphanBuildContainers(ctx))
	_, err = cli.ContainerInspect(ctx, created.ID)
	require.Error(t, err)
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func TestAdapterRejectsUnprotectedBuildNetworkSmoke(t *testing.T) {
	testenv.Enabled(t, "BUILDER_TEST_DOCKER")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer func() { _ = cli.Close() }() // test teardown; close failure is not actionable
	created, err := cli.NetworkCreate(ctx, "dcm-unprotected-test-"+uuid.NewString(), network.CreateOptions{Driver: "bridge"})
	require.NoError(t, err)
	defer func() { _ = cli.NetworkRemove(context.Background(), created.ID) }() // best-effort test network removal
	adapter, err := builderdocker.NewAdapter(builderdocker.Config{RequireNetworkPolicy: true})
	require.NoError(t, err)
	defer func() { _ = adapter.Close() }() // test teardown; close failure is not actionable
	require.ErrorIs(t, adapter.ValidateBuildNetwork(ctx, created.ID), apperrors.ErrUnavailable)
}
