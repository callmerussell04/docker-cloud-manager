package docker_test

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	builderdocker "github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/require"
)

func TestAdapterCleanupOrphanBuildContainersSmoke(t *testing.T) {
	if os.Getenv("BUILDER_TEST_DOCKER") != "1" {
		t.Skip("set BUILDER_TEST_DOCKER=1 to run Docker smoke test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	imageName := getenv("BUILDER_TEST_DOCKER_IMAGE", "busybox:latest")
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
			"managed_by":        "docker-cloud-manager",
			"dcm.resource_type": "build",
			"dcm.build_id":      "builder-smoke-test",
		},
	}, nil, nil, nil, "")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = cli.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true})
	})

	adapter, err := builderdocker.NewAdapter()
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
