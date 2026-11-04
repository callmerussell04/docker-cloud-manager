package docker_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/telemetry/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/telemetry/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dockertest"
)

func testTarget() model.ContainerTarget {
	return model.ContainerTarget{
		ContainerID:      "00000000-0000-0000-0000-000000000001",
		DockerID:         "docker-id",
		OwnerID:          "11111111-1111-1111-1111-111111111111",
		DockerGeneration: 2,
	}
}

func matchingLabels(target model.ContainerTarget) map[string]string {
	return map[string]string{
		"managed_by":        "docker-cloud-manager",
		"dcm.resource_type": "container",
		"dcm.container_id":  target.ContainerID,
		"dcm.owner_id":      target.OwnerID,
		"dcm.generation":    strconv.Itoa(target.DockerGeneration),
	}
}

func inspectBody(t *testing.T, labels map[string]string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"Id": "docker-id", "Config": map[string]any{"Labels": labels}})
	require.NoError(t, err)
	return string(body)
}

// The label check is the last ownership check before a log or terminal stream
// opens: every identity label must match the target Core resolved.
func TestAdapterEnsureContainerTargetChecksIdentityLabels(t *testing.T) {
	for _, tt := range []struct {
		name    string
		label   string
		value   string
		remove  bool
		wantErr bool
	}{
		{name: "matching labels"},
		{name: "another owner", label: "dcm.owner_id", value: "22222222-2222-2222-2222-222222222222", wantErr: true},
		{name: "missing owner", label: "dcm.owner_id", remove: true, wantErr: true},
		{name: "another container", label: "dcm.container_id", value: "00000000-0000-0000-0000-000000000002", wantErr: true},
		{name: "missing container", label: "dcm.container_id", remove: true, wantErr: true},
		{name: "older generation", label: "dcm.generation", value: "1", wantErr: true},
		{name: "missing generation", label: "dcm.generation", remove: true, wantErr: true},
		{name: "volume resource", label: "dcm.resource_type", value: "volume", wantErr: true},
		{name: "build resource", label: "dcm.resource_type", value: "build", wantErr: true},
		{name: "missing resource type", label: "dcm.resource_type", remove: true, wantErr: true},
		{name: "unmanaged container", label: "managed_by", value: "someone-else", wantErr: true},
		{name: "missing managed-by marker", label: "managed_by", remove: true, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			labels := matchingLabels(testTarget())
			if tt.remove {
				delete(labels, tt.label)
			} else if tt.label != "" {
				labels[tt.label] = tt.value
			}
			dockertest.Serve(t, map[string]dockertest.Response{
				"/containers/docker-id/json": dockertest.OK(inspectBody(t, labels)),
			})
			adapter := newAdapter(t)

			err := adapter.EnsureContainerTarget(context.Background(), testTarget())

			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, apperrors.ErrNotFound)
		})
	}
}

func TestAdapterEnsureContainerTargetRejectsUnknownContainers(t *testing.T) {
	for _, tt := range []struct {
		name     string
		response dockertest.Response
	}{
		{name: "missing container", response: dockertest.Error(http.StatusNotFound, "No such container: docker-id")},
		{name: "container without config", response: dockertest.OK(`{"Id":"docker-id"}`)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dockertest.Serve(t, map[string]dockertest.Response{"/containers/docker-id/json": tt.response})
			adapter := newAdapter(t)

			require.ErrorIs(t, adapter.EnsureContainerTarget(context.Background(), testTarget()), apperrors.ErrNotFound)
		})
	}
}

// A terminal runs the requested command with a TTY as the container's own
// user: never privileged, and with no environment or user injected.
func TestAdapterOpenTerminalRequestsUnprivilegedTTYExec(t *testing.T) {
	daemon := dockertest.Serve(t, map[string]dockertest.Response{
		"/containers/docker-id/exec": dockertest.Created(`{"Id":"exec-id"}`),
		// Failing here ends the open once the exec request is recorded.
		"/exec/exec-id/start": dockertest.Error(http.StatusInternalServerError, "stop after exec create"),
	})
	adapter := newAdapter(t)

	_, err := adapter.OpenTerminal(context.Background(), testTarget(), model.TerminalOptions{
		Command: []string{"/bin/sh"},
		Rows:    24,
		Cols:    80,
	})
	require.Error(t, err)

	execs := daemon.Requests("/containers/docker-id/exec")
	require.Len(t, execs, 1)
	var exec map[string]any
	require.NoError(t, json.Unmarshal(execs[0].Body, &exec))
	require.Equal(t, true, exec["Tty"])
	require.Equal(t, true, exec["AttachStdin"])
	require.Equal(t, []any{"/bin/sh"}, exec["Cmd"])
	require.Equal(t, []any{float64(24), float64(80)}, exec["ConsoleSize"])
	require.NotEqual(t, true, exec["Privileged"])
	require.Empty(t, exec["Env"])
	require.Empty(t, exec["User"])
	require.Empty(t, exec["WorkingDir"])
}

func newAdapter(t *testing.T) *docker.Adapter {
	t.Helper()
	adapter, err := docker.NewAdapter()
	require.NoError(t, err)
	t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable
	return adapter
}
