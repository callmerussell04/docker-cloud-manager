package docker_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dockertest"
)

// The spec Core hands Docker is what enforces ownership recovery, admission
// limits and bounded log disk use; nothing downstream corrects it.
func TestAdapterCreateContainerSendsIdentityLimitsAndLogOptions(t *testing.T) {
	daemon := dockertest.Serve(t, map[string]dockertest.Response{
		"/images/registry:5000/owner_app:latest/json": dockertest.OK(`{"Id":"sha256:checked","Config":{"Labels":{"org.opencontainers.image.title":"app"}}}`),
		"/containers/create":                          dockertest.Created(`{"Id":"docker-id"}`),
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
		Image      string
		Labels     map[string]string
		HostConfig struct {
			Memory            int64
			MemorySwap        int64
			MemoryReservation int64
			CPUShares         int64
			CPUQuota          int64
			CPUPeriod         int64
			PidsLimit         *int64
			CgroupParent      string
			SecurityOpt       []string
			CapDrop           []string
			LogConfig         struct {
				Type   string
				Config map[string]string
			}
		}
	}
	require.NoError(t, json.Unmarshal(created[0].Body, &spec))
	require.Equal(t, "sha256:checked", spec.Image, "the inspected image runs, not whatever the tag points at by then")
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
	require.Empty(t, host.CgroupParent, "without a configured parent Docker chooses the cgroup")
	require.Equal(t, []string{"no-new-privileges:true"}, host.SecurityOpt)
	require.Equal(t, []string{"CAP_NET_RAW"}, host.CapDrop) // the client normalizes capability names
	require.Equal(t, "json-file", host.LogConfig.Type)
	require.Equal(t, map[string]string{"max-size": "10m", "max-file": "3"}, host.LogConfig.Config)
}

// The parent cgroup is where the host caps what user containers take
// together, so every container the adapter creates must be placed under it.
func TestAdapterCreateContainerPlacesItUnderTheConfiguredCgroupParent(t *testing.T) {
	daemon := dockertest.Serve(t, map[string]dockertest.Response{
		"/images/nginx:latest/json": dockertest.OK(`{"Id":"sha256:plain"}`),
		"/containers/create":        dockertest.Created(`{"Id":"docker-id"}`),
	})
	adapter, err := docker.NewAdapter(docker.WithCgroupParent(" dcm-tenants.slice "))
	require.NoError(t, err)
	t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable

	_, err = adapter.CreateContainer(context.Background(), model.ContainerRuntimeSpec{
		ContainerName: "usr_app", ImageName: "nginx:latest", NetworkName: "dcm_user_net",
	})
	require.NoError(t, err)

	created := daemon.Requests("/containers/create")
	require.Len(t, created, 1)
	var spec struct{ HostConfig struct{ CgroupParent string } }
	require.NoError(t, json.Unmarshal(created[0].Body, &spec))
	require.Equal(t, "dcm-tenants.slice", spec.HostConfig.CgroupParent)
}

// The working set is usage less the file cache the kernel can drop, so a
// container is not counted as short of memory for cache it merely holds. The
// same sample carries the current memory limit and cumulative CPU time.
func TestAdapterGetContainerUsage(t *testing.T) {
	for _, tc := range []struct {
		name           string
		stats          string
		wantWorkingSet int64
	}{
		{name: "cgroup v2 subtracts inactive file cache", stats: `{"memory_stats":{"usage":1000,"limit":8000,"stats":{"inactive_file":300,"total_inactive_file":999}},"cpu_stats":{"cpu_usage":{"total_usage":123456789}}}`, wantWorkingSet: 700},
		{name: "cgroup v1 subtracts total inactive file cache", stats: `{"memory_stats":{"usage":1000,"limit":8000,"stats":{"total_inactive_file":250}},"cpu_stats":{"cpu_usage":{"total_usage":123456789}}}`, wantWorkingSet: 750},
		{name: "no cache figure reports usage", stats: `{"memory_stats":{"usage":1000,"limit":8000},"cpu_stats":{"cpu_usage":{"total_usage":123456789}}}`, wantWorkingSet: 1000},
		{name: "cache above usage reports usage", stats: `{"memory_stats":{"usage":1000,"limit":8000,"stats":{"inactive_file":4000}},"cpu_stats":{"cpu_usage":{"total_usage":123456789}}}`, wantWorkingSet: 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			daemon := dockertest.Serve(t, map[string]dockertest.Response{
				"/containers/docker-id/stats": dockertest.OK(tc.stats),
			})
			adapter, err := docker.NewAdapter()
			require.NoError(t, err)
			t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable

			got, err := adapter.GetContainerUsage(context.Background(), "docker-id")

			require.NoError(t, err)
			require.Equal(t, model.ContainerUsage{WorkingSetBytes: tc.wantWorkingSet, MemoryLimitBytes: 8000, CPUUsageNanoseconds: 123456789}, got)
			requests := daemon.Requests("/containers/docker-id/stats")
			require.Len(t, requests, 1)
			require.Equal(t, "1", requests[0].Query.Get("one-shot"), "one sample, without waiting for a second")
		})
	}
}

// The memory pressure worker attributes a build container to its build and
// owner by the labels Builder gives it, and asks only for running ones. A
// container whose identity labels do not parse is not a build Core can
// attribute, so it is left out.
func TestAdapterListRunningBuildContainers(t *testing.T) {
	buildID := "8f0e7a52-6a1b-4f36-9d6c-3c1f3d1f0a11"
	ownerID := "2b7c1d4e-0f5a-4c3b-8e9d-7a6b5c4d3e2f"
	daemon := dockertest.Serve(t, map[string]dockertest.Response{
		"/containers/json": dockertest.OK(`[
			{"Id":"kaniko-1","Labels":{"managed_by":"docker-cloud-manager","dcm.resource_type":"build","dcm.build_id":"` + buildID + `","dcm.owner_id":"` + ownerID + `"}},
			{"Id":"kaniko-bad","Labels":{"managed_by":"docker-cloud-manager","dcm.resource_type":"build","dcm.build_id":"not-a-uuid","dcm.owner_id":"` + ownerID + `"}}
		]`),
	})
	adapter, err := docker.NewAdapter()
	require.NoError(t, err)
	t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable

	got, err := adapter.ListRunningBuildContainers(context.Background())

	require.NoError(t, err)
	require.Equal(t, []model.BuildContainer{{
		DockerID: "kaniko-1",
		BuildID:  uuid.MustParse(buildID),
		OwnerID:  uuid.MustParse(ownerID),
	}}, got)
	requests := daemon.Requests("/containers/json")
	require.Len(t, requests, 1)
	var filter map[string]map[string]bool
	require.NoError(t, json.Unmarshal([]byte(requests[0].Query.Get("filters")), &filter))
	require.Equal(t, map[string]map[string]bool{
		"label":  {"managed_by=docker-cloud-manager": true, "dcm.resource_type=build": true},
		"status": {"running": true},
	}, filter)
}

// Docker copies an image's labels onto its containers, and Traefik's Docker
// provider reads every traefik.* label on a container it serves. A tenant's
// image must therefore never bring its own: a router label would claim the
// platform's host or another tenant's.
func TestAdapterCreateContainerRejectsImagesDeclaringTraefikLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels string
	}{
		{name: "router rule", labels: "{\"traefik.http.routers.evil.rule\":\"Host(`dcm.example`)\"}"},
		{name: "enable flag", labels: `{"traefik.enable":"true"}`},
		{name: "mixed case root", labels: `{"Traefik.HTTP.Routers.evil.priority":"1000"}`},
		{name: "bare root", labels: `{"traefik":"x"}`},
		{name: "beside harmless labels", labels: `{"maintainer":"someone","traefik.docker.network":"proxy_net"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// No create route: the fake fails the test if a container is created.
			dockertest.Serve(t, map[string]dockertest.Response{
				"/images/attacker/app:latest/json": dockertest.OK(`{"Id":"sha256:evil","Config":{"Labels":` + tc.labels + `}}`),
			})
			adapter, err := docker.NewAdapter()
			require.NoError(t, err)
			t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable

			_, err = adapter.CreateContainer(context.Background(), model.ContainerRuntimeSpec{
				ContainerName: "usr_app", ImageName: "attacker/app:latest", NetworkName: "dcm_user_net",
				Domain: "app.dcm.example", InternalPort: 8080, ProxyNetworkName: "proxy_net",
			})

			require.ErrorIs(t, err, apperrors.ErrBadRequest)
			require.NotContains(t, err.Error(), "evil", "label keys are image content and are not echoed")
		})
	}
}

func TestAdapterCreateContainerAcceptsImagesWithoutTraefikLabels(t *testing.T) {
	tests := []struct {
		name  string
		image string
	}{
		{name: "no config", image: `{"Id":"sha256:plain"}`},
		{name: "no labels", image: `{"Id":"sha256:plain","Config":{}}`},
		{name: "similar root", image: `{"Id":"sha256:plain","Config":{"Labels":{"traefikish.enable":"true","com.example.traefik.enable":"true"}}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			daemon := dockertest.Serve(t, map[string]dockertest.Response{
				"/images/nginx:latest/json": dockertest.OK(tc.image),
				"/containers/create":        dockertest.Created(`{"Id":"docker-id"}`),
			})
			adapter, err := docker.NewAdapter()
			require.NoError(t, err)
			t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable

			dockerID, err := adapter.CreateContainer(context.Background(), model.ContainerRuntimeSpec{
				ContainerName: "usr_app", ImageName: "nginx:latest", NetworkName: "dcm_user_net",
			})

			require.NoError(t, err)
			require.Equal(t, "docker-id", dockerID)
			require.Len(t, daemon.Requests("/containers/create"), 1)
		})
	}
}

// An image Docker reports without an ID cannot be pinned, so nothing is created
// from its tag instead.
func TestAdapterCreateContainerRefusesAnImageWithoutAnID(t *testing.T) {
	dockertest.Serve(t, map[string]dockertest.Response{
		"/images/nginx:latest/json": dockertest.OK(`{"Config":{"Labels":{}}}`),
	})
	adapter, err := docker.NewAdapter()
	require.NoError(t, err)
	t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable

	_, err = adapter.CreateContainer(context.Background(), model.ContainerRuntimeSpec{
		ContainerName: "usr_app", ImageName: "nginx:latest", NetworkName: "dcm_user_net",
	})

	require.Error(t, err)
}

// The root filesystem cap is a storage option only some storage setups honour;
// on others Docker accepts it and ignores it. The status tells an
// administrator which kind of host this is.
func TestAdapterReadRootfsQuotaStatus(t *testing.T) {
	tests := []struct {
		name    string
		info    dockertest.Response
		want    model.RootfsQuotaStatus
		wantErr bool
	}{
		{
			name: "overlay2 on XFS",
			info: dockertest.OK(`{"Driver":"overlay2","DriverStatus":[["Backing Filesystem","xfs"],["Supports d_type","true"]]}`),
			want: model.RootfsQuotaEnforced,
		},
		{
			name: "overlay2 on ext4",
			info: dockertest.OK(`{"Driver":"overlay2","DriverStatus":[["Backing Filesystem","extfs"],["Supports d_type","true"]]}`),
			want: model.RootfsQuotaNotEnforced,
		},
		{
			name: "overlay2 without a reported backing filesystem",
			info: dockertest.OK(`{"Driver":"overlay2","DriverStatus":[["Supports d_type","true"]]}`),
			want: model.RootfsQuotaUnknown,
		},
		{
			name: "containerd snapshotter",
			info: dockertest.OK(`{"Driver":"overlayfs","DriverStatus":[["driver-type","io.containerd.snapshotter.v1"]]}`),
			want: model.RootfsQuotaNotEnforced,
		},
		{
			name: "containerd snapshotter even when the backing filesystem is XFS",
			info: dockertest.OK(`{"Driver":"overlayfs","DriverStatus":[["driver-type","io.containerd.snapshotter.v1"],["Backing Filesystem","xfs"]]}`),
			want: model.RootfsQuotaNotEnforced,
		},
		{
			name: "btrfs",
			info: dockertest.OK(`{"Driver":"btrfs","DriverStatus":[["Build Version","Btrfs v6.6"]]}`),
			want: model.RootfsQuotaEnforced,
		},
		{
			name: "zfs",
			info: dockertest.OK(`{"Driver":"zfs","DriverStatus":[["Zpool","tank"]]}`),
			want: model.RootfsQuotaEnforced,
		},
		{
			name: "devicemapper",
			info: dockertest.OK(`{"Driver":"devicemapper","DriverStatus":[["Pool Name","docker-thinpool"]]}`),
			want: model.RootfsQuotaEnforced,
		},
		{
			name: "an unlisted driver",
			info: dockertest.OK(`{"Driver":"vfs","DriverStatus":[]}`),
			want: model.RootfsQuotaUnknown,
		},
		{
			name:    "the info cannot be read",
			info:    dockertest.Error(500, "daemon is starting"),
			want:    model.RootfsQuotaUnknown,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			daemon := dockertest.Serve(t, map[string]dockertest.Response{"GET /info": tt.info})
			adapter, err := docker.NewAdapter()
			require.NoError(t, err)
			t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable

			status, err := adapter.ReadRootfsQuotaStatus(context.Background())

			require.Equal(t, tt.want, status)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Len(t, daemon.Requests("/info"), 1)
		})
	}
}

// A volume is given a size only when Core asks for one: that is what turns
// the kernel limit on, and a measured volume must stay unsized.
func TestAdapterCreateVolumeSendsTheSizeOnlyWhenAsked(t *testing.T) {
	tests := []struct {
		name       string
		sizeBytes  int64
		wantDriver string
		wantOpts   map[string]string
	}{
		{name: "measured volume", sizeBytes: 0},
		{name: "enforced volume", sizeBytes: 64 << 20, wantDriver: "local", wantOpts: map[string]string{"size": "67108864"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			daemon := dockertest.Serve(t, map[string]dockertest.Response{
				"POST /volumes/create": dockertest.Created(`{"Name":"vol_owner_data","Driver":"local"}`),
			})
			adapter, err := docker.NewAdapter()
			require.NoError(t, err)
			t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable

			name, err := adapter.CreateVolume(context.Background(), model.VolumeRuntimeSpec{
				VolumeName: "vol_owner_data", VolumeID: "volume-1", OwnerID: "owner-1", Name: "data", SizeBytes: tt.sizeBytes,
			})
			require.NoError(t, err)
			require.Equal(t, "vol_owner_data", name)

			requests := daemon.Requests("/volumes/create")
			require.Len(t, requests, 1)
			var sent struct {
				Name       string
				Driver     string
				DriverOpts map[string]string
				Labels     map[string]string
			}
			require.NoError(t, json.Unmarshal(requests[0].Body, &sent))
			require.Equal(t, "vol_owner_data", sent.Name)
			require.Equal(t, tt.wantDriver, sent.Driver)
			require.Equal(t, tt.wantOpts, sent.DriverOpts)
			require.Equal(t, "volume-1", sent.Labels["dcm.volume_id"])
			require.Equal(t, "owner-1", sent.Labels["dcm.owner_id"])
			require.Equal(t, "data", sent.Labels["dcm.volume_name"], "the host shows the user-facing name")
		})
	}
}

// The host check creates a sized throwaway volume and always removes it, so
// checks leave nothing behind. Docker refusing the size means "not supported";
// anything else that goes wrong is an error.
func TestAdapterCheckVolumeSizeSupport(t *testing.T) {
	tests := []struct {
		name          string
		create        dockertest.Response
		remove        dockertest.Response
		wantSupported bool
		wantErr       bool
	}{
		{
			name:          "Docker accepts the size",
			create:        dockertest.Created(`{"Name":"check","Driver":"local","Options":{"size":"16777216"}}`),
			remove:        dockertest.NoContent(),
			wantSupported: true,
		},
		{
			name:   "Docker refuses the size",
			create: dockertest.Error(400, "quota size requested but no quota support"),
			remove: dockertest.Error(404, "no such volume"),
		},
		{
			name:    "the create fails for another reason",
			create:  dockertest.Error(500, "daemon is shutting down"),
			remove:  dockertest.Error(404, "no such volume"),
			wantErr: true,
		},
		{
			name:    "the volume is created but cannot be removed",
			create:  dockertest.Created(`{"Name":"check","Driver":"local","Options":{"size":"16777216"}}`),
			remove:  dockertest.Error(500, "volume is busy"),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			daemon := dockertest.Serve(t, map[string]dockertest.Response{
				"POST /volumes/create": tt.create,
				"DELETE /volumes/*":    tt.remove,
			})
			adapter, err := docker.NewAdapter()
			require.NoError(t, err)
			t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable

			supported, err := adapter.CheckVolumeSizeSupport(context.Background())

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantSupported, supported)

			creates := daemon.Requests("/volumes/create")
			require.Len(t, creates, 1)
			var sent struct {
				Name       string
				Driver     string
				DriverOpts map[string]string
				Labels     map[string]string
			}
			require.NoError(t, json.Unmarshal(creates[0].Body, &sent))
			require.NotEmpty(t, sent.Name)
			require.Equal(t, "local", sent.Driver)
			require.NotEmpty(t, sent.DriverOpts["size"])
			require.Equal(t, "docker-cloud-manager", sent.Labels["managed_by"])

			removes := daemon.Requests("/volumes/" + sent.Name)
			require.Len(t, removes, 1, "the check volume is removed on every path")
			require.Equal(t, "DELETE", removes[0].Method)
			require.Equal(t, "1", removes[0].Query.Get("force"))
		})
	}
}

// Registry garbage collection runs as a detached exec in the Registry
// container, deleting untagged manifests; its state is read back by exec ID.
func TestAdapterStartsAndInspectsRegistryGarbageCollect(t *testing.T) {
	daemon := dockertest.Serve(t, map[string]dockertest.Response{
		"/containers/registry/exec": dockertest.Created(`{"Id":"exec-id"}`),
		"/exec/exec-id/start":       dockertest.OK(``),
		"/exec/exec-id/json":        dockertest.OK(`{"ID":"exec-id","Running":false,"ExitCode":3}`),
	})
	adapter, err := docker.NewAdapter()
	require.NoError(t, err)
	t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable

	execID, err := adapter.StartRegistryGarbageCollect(context.Background(), "registry")
	require.NoError(t, err)
	require.Equal(t, "exec-id", execID)
	execs := daemon.Requests("/containers/registry/exec")
	require.Len(t, execs, 1)
	var exec struct{ Cmd []string }
	require.NoError(t, json.Unmarshal(execs[0].Body, &exec))
	require.Equal(t, []string{"/bin/registry", "garbage-collect", "/etc/docker/registry/config.yml", "--delete-untagged=true"}, exec.Cmd)
	require.Len(t, daemon.Requests("/exec/exec-id/start"), 1)

	running, exitCode, err := adapter.InspectRegistryGarbageCollect(context.Background(), execID)
	require.NoError(t, err)
	require.False(t, running)
	require.Equal(t, 3, exitCode)
}

// An exec the daemon no longer knows, as after a Registry restart, is not
// found rather than an upstream failure, so the GC worker can stop waiting.
func TestAdapterInspectRegistryGarbageCollectReportsAGoneExecAsNotFound(t *testing.T) {
	dockertest.Serve(t, map[string]dockertest.Response{
		"/exec/exec-id/json": dockertest.Error(http.StatusNotFound, "No such exec instance: exec-id"),
	})
	adapter, err := docker.NewAdapter()
	require.NoError(t, err)
	t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable

	_, _, err = adapter.InspectRegistryGarbageCollect(context.Background(), "exec-id")
	require.ErrorIs(t, err, apperrors.ErrNotFound)
}
