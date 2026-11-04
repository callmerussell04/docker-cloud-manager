package docker_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dockertest"
)

// The spec Core hands Docker is what enforces ownership recovery, admission
// limits and bounded log disk use; nothing downstream corrects it.
func TestAdapterCreateContainerSendsIdentityLimitsAndLogOptions(t *testing.T) {
	daemon := dockertest.Serve(t, map[string]dockertest.Response{
		"/containers/create": dockertest.Created(`{"Id":"docker-id"}`),
	})
	adapter, err := docker.NewAdapter()
	require.NoError(t, err)
	t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable

	dockerID, err := adapter.CreateContainer(context.Background(), model.ContainerRuntimeSpec{
		ContainerID:          "container-uuid",
		OwnerID:              "owner-uuid",
		ProjectID:            "project-uuid",
		Generation:           3,
		ContainerName:        "dcm_app",
		ImageName:            "registry:5000/owner_app:latest",
		NetworkName:          "dcm_user_net",
		MemoryLimitBytes:     256 << 20,
		MemoryReservation:    128 << 20,
		MemorySwapMultiplier: 1.5,
		CPUShares:            512,
		CPUQuota:             50000,
		CPUPeriod:            100000,
		PidsLimit:            200,
		MaxLogSize:           "10m",
		MaxLogFiles:          "3",
	})
	require.NoError(t, err)
	require.Equal(t, "docker-id", dockerID)

	created := daemon.Requests("/containers/create")
	require.Len(t, created, 1)
	require.Equal(t, "dcm_app", created[0].Query.Get("name"))
	var spec struct {
		Labels     map[string]string
		HostConfig struct {
			Memory            int64
			MemorySwap        int64
			MemoryReservation int64
			CPUShares         int64
			CPUQuota          int64
			CPUPeriod         int64
			PidsLimit         *int64
			SecurityOpt       []string
			CapDrop           []string
			LogConfig         struct {
				Type   string
				Config map[string]string
			}
		}
	}
	require.NoError(t, json.Unmarshal(created[0].Body, &spec))
	require.Equal(t, map[string]string{
		"managed_by":        "docker-cloud-manager",
		"dcm.resource_type": "container",
		"dcm.container_id":  "container-uuid",
		"dcm.owner_id":      "owner-uuid",
		"dcm.project_id":    "project-uuid",
		"dcm.generation":    "3",
	}, spec.Labels)
	host := spec.HostConfig
	require.Equal(t, int64(256<<20), host.Memory)
	require.Equal(t, int64(384<<20), host.MemorySwap)
	require.Equal(t, int64(128<<20), host.MemoryReservation)
	require.Equal(t, int64(512), host.CPUShares)
	require.Equal(t, int64(50000), host.CPUQuota)
	require.Equal(t, int64(100000), host.CPUPeriod)
	require.NotNil(t, host.PidsLimit)
	require.Equal(t, int64(200), *host.PidsLimit)
	require.Equal(t, []string{"no-new-privileges:true"}, host.SecurityOpt)
	require.Equal(t, []string{"CAP_NET_RAW"}, host.CapDrop) // the client normalizes capability names
	require.Equal(t, "json-file", host.LogConfig.Type)
	require.Equal(t, map[string]string{"max-size": "10m", "max-file": "3"}, host.LogConfig.Config)
}
