package docker_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/testenv"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAdapterContainerStreamsSmoke(t *testing.T) {
	testenv.Enabled(t, "CORE_TEST_DOCKER")

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	imageName := getenv("CORE_TEST_DOCKER_IMAGE", "busybox:latest")
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })

	pull, err := cli.ImagePull(ctx, imageName, image.PullOptions{})
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, pull)
	require.NoError(t, err)
	require.NoError(t, pull.Close())

	target := model.ContainerRuntimeTarget{
		ContainerID:      uuid.New(),
		Status:           "exited",
		OwnerID:          uuid.New(),
		DockerGeneration: 2,
	}
	created, err := cli.ContainerCreate(ctx, &container.Config{
		Image: imageName,
		Cmd:   []string{"sh", "-c", "echo stdout; echo stderr >&2"},
		Labels: map[string]string{
			"managed_by":        "docker-cloud-manager",
			"dcm.resource_type": "container",
			"dcm.container_id":  target.ContainerID.String(),
			"dcm.owner_id":      target.OwnerID.String(),
			"dcm.generation":    "2",
		},
	}, nil, nil, nil, "")
	require.NoError(t, err)
	target.DockerID = created.ID
	t.Cleanup(func() {
		_ = cli.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true})
	})

	require.NoError(t, cli.ContainerStart(ctx, created.ID, container.StartOptions{}))
	waitStatus, waitErr := cli.ContainerWait(ctx, created.ID, container.WaitConditionNotRunning)
	select {
	case result := <-waitStatus:
		require.Equal(t, int64(0), result.StatusCode)
	case err := <-waitErr:
		require.NoError(t, err)
	case <-ctx.Done():
		require.NoError(t, ctx.Err())
	}

	adapter, err := docker.NewAdapter()
	require.NoError(t, err)
	t.Cleanup(func() { _ = adapter.Close() })

	require.NoError(t, adapter.EnsureContainerTarget(ctx, target))
	mismatchedTarget := target
	mismatchedTarget.OwnerID = uuid.New()
	require.ErrorIs(t, adapter.EnsureContainerTarget(ctx, mismatchedTarget), apperrors.ErrNotFound)

	stream, err := adapter.StreamContainerLogs(ctx, target.DockerID, model.LogOptions{Tail: 20, Follow: false})
	require.NoError(t, err)
	defer func() { _ = stream.Close() }() // read-only stream; close failure is not actionable
	body, err := io.ReadAll(stream)
	require.NoError(t, err)
	require.True(t, strings.Contains(string(body), "stdout") && strings.Contains(string(body), "stderr"), string(body))
}
