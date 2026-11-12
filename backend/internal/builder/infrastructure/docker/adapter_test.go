package docker_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/stretchr/testify/require"

	builderdocker "github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dockertest"
)

const (
	isolatedBuildNetwork = `{"Name":"build_net","Internal":true,"EnableIPv6":false,"Options":{"com.docker.network.bridge.name":"br-dcm-build"}}`
	policyContainers     = `[{"Id":"policy"}]`
	healthyPolicy        = `{"Id":"policy","State":{"Running":true,"Health":{"Status":"healthy"}}}`
)

// A RUN step inherits nothing from the Kaniko process environment, so only
// build args give it the route out of the internal build network.
func TestAdapterRunBuildContainerPassesProxyToRunSteps(t *testing.T) {
	const egress = "http://build-egress:3128"
	proxyArgs := []string{
		"--build-arg=HTTPS_PROXY=" + egress,
		"--build-arg=HTTP_PROXY=" + egress,
		"--build-arg=NO_PROXY=registry:5000",
		"--build-arg=http_proxy=" + egress,
		"--build-arg=https_proxy=" + egress,
		"--build-arg=no_proxy=registry:5000",
	}
	for _, tt := range []struct {
		name      string
		buildArgs map[string]string
		want      []string
	}{
		{name: "without user args", want: proxyArgs},
		{
			name:      "user args are kept",
			buildArgs: map[string]string{"NODE_ENV": "production"},
			want: []string{
				"--build-arg=HTTPS_PROXY=" + egress,
				"--build-arg=HTTP_PROXY=" + egress,
				"--build-arg=NODE_ENV=production",
				"--build-arg=NO_PROXY=registry:5000",
				"--build-arg=http_proxy=" + egress,
				"--build-arg=https_proxy=" + egress,
				"--build-arg=no_proxy=registry:5000",
			},
		},
		{
			name:      "user proxy args cannot replace the egress proxy",
			buildArgs: map[string]string{"HTTP_PROXY": "http://other:1", "no_proxy": "*"},
			want:      proxyArgs,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			daemon := dockertest.Serve(t, map[string]dockertest.Response{
				"/networks/build_net": dockertest.OK(isolatedBuildNetwork),
				"/images/kaniko/json": dockertest.OK(`{"Id":"kaniko"}`),
				// Failing here ends the run once the container request is recorded.
				"/containers/create": dockertest.Error(http.StatusInternalServerError, "stop after create"),
			})
			adapter := newAdapter(t, builderdocker.Config{RegistryCAFile: dockertest.RegistryCAFile(t)})

			_, _, err := adapter.RunBuildContainer(context.Background(), model.BuildRuntimeSpec{
				RegistryUsername: "builder",
				RegistryPassword: "password",
				DestinationTag:   "registry:5000/owner_image:latest",
				KanikoImage:      "kaniko",
				Dockerfile:       "Dockerfile",
				NetworkName:      "build_net",
				BuildArgs:        tt.buildArgs,
			})
			require.Error(t, err)

			var request struct{ Cmd []string }
			created := daemon.Requests("/containers/create")
			require.Len(t, created, 1)
			require.NoError(t, json.Unmarshal(created[0].Body, &request))
			var got []string
			for _, arg := range request.Cmd {
				if strings.HasPrefix(arg, "--build-arg=") {
					got = append(got, arg)
				}
			}
			require.Equal(t, tt.want, got)
		})
	}
}

// Build containers run user Dockerfiles, so they sit under the same parent
// cgroup as user containers and count against the same host cap.
func TestAdapterRunBuildContainerPlacesItUnderTheConfiguredCgroupParent(t *testing.T) {
	for _, tt := range []struct {
		name   string
		parent string
		want   string
	}{
		{name: "configured parent", parent: "dcm-tenants.slice", want: "dcm-tenants.slice"},
		{name: "no parent keeps Docker's default", parent: "", want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			daemon := dockertest.Serve(t, map[string]dockertest.Response{
				"/networks/build_net": dockertest.OK(isolatedBuildNetwork),
				"/images/kaniko/json": dockertest.OK(`{"Id":"kaniko"}`),
				// Failing here ends the run once the container request is recorded.
				"/containers/create": dockertest.Error(http.StatusInternalServerError, "stop after create"),
			})
			adapter := newAdapter(t, builderdocker.Config{RegistryCAFile: dockertest.RegistryCAFile(t), CgroupParent: tt.parent})

			_, _, err := adapter.RunBuildContainer(context.Background(), model.BuildRuntimeSpec{
				RegistryUsername: "builder",
				RegistryPassword: "password",
				DestinationTag:   "registry:5000/owner_image:latest",
				KanikoImage:      "kaniko",
				Dockerfile:       "Dockerfile",
				NetworkName:      "build_net",
			})
			require.Error(t, err)

			created := daemon.Requests("/containers/create")
			require.Len(t, created, 1)
			var request struct{ HostConfig struct{ CgroupParent string } }
			require.NoError(t, json.Unmarshal(created[0].Body, &request))
			require.Equal(t, tt.want, request.HostConfig.CgroupParent)
		})
	}
}

func TestAdapterValidateBuildNetwork(t *testing.T) {
	notFound := dockertest.Error(http.StatusNotFound, "network build_net not found")
	daemonFailure := dockertest.Error(http.StatusInternalServerError, "daemon failure")
	for _, tt := range []struct {
		name      string
		network   dockertest.Response
		list      dockertest.Response
		policy    dockertest.Response
		wantErr   string
		wantCause func(error) bool
	}{
		{
			name:    "isolated network with healthy policy",
			network: dockertest.OK(isolatedBuildNetwork), list: dockertest.OK(policyContainers), policy: dockertest.OK(healthyPolicy),
		},
		{
			name:      "missing network keeps inspection cause",
			network:   notFound,
			wantErr:   "build network inspection failed",
			wantCause: cerrdefs.IsNotFound,
		},
		{
			name:    "unisolated network",
			network: dockertest.OK(`{"Name":"build_net","Internal":false,"Options":{"com.docker.network.bridge.name":"br-dcm-build"}}`),
			wantErr: "build network is not isolated",
		},
		{
			name:      "policy listing failure keeps cause",
			network:   dockertest.OK(isolatedBuildNetwork),
			list:      daemonFailure,
			wantErr:   "build network policy is unavailable",
			wantCause: cerrdefs.IsInternal,
		},
		{
			name:      "policy inspection failure keeps cause",
			network:   dockertest.OK(isolatedBuildNetwork),
			list:      dockertest.OK(policyContainers),
			policy:    daemonFailure,
			wantErr:   "build network policy is unavailable",
			wantCause: cerrdefs.IsInternal,
		},
		{
			name:    "unhealthy policy",
			network: dockertest.OK(isolatedBuildNetwork), list: dockertest.OK(policyContainers),
			policy:  dockertest.OK(`{"Id":"policy","State":{"Running":true,"Health":{"Status":"starting"}}}`),
			wantErr: "build network policy is unavailable",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dockertest.Serve(t, map[string]dockertest.Response{
				"/networks/build_net":     tt.network,
				"/containers/json":        tt.list,
				"/containers/policy/json": tt.policy,
			})
			adapter := newAdapter(t, builderdocker.Config{RequireNetworkPolicy: true})

			err := adapter.ValidateBuildNetwork(context.Background(), "build_net")

			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, apperrors.ErrUnavailable)
			require.EqualError(t, err, tt.wantErr)
			if tt.wantCause != nil {
				require.True(t, tt.wantCause(err), "cause missing from %v", err)
			}
		})
	}
}

// Scoped Registry credentials reach Kaniko only through the 0600 archive
// copied to the container root, never through its environment or command,
// which `docker inspect` shows to anyone with daemon access.
func TestAdapterRunBuildContainerKeepsCredentialsInRestrictedArchive(t *testing.T) {
	const (
		username = "build-user"
		password = "scoped-registry-password"
	)
	daemon := dockertest.Serve(t, map[string]dockertest.Response{
		"/images/kaniko/json":                 dockertest.OK(`{"Id":"kaniko"}`),
		"/containers/create":                  dockertest.Created(`{"Id":"build-container"}`),
		"/containers/build-container/archive": dockertest.OK(""),
		"/containers/build-container/start":   dockertest.NoContent(),
		"/containers/build-container/logs":    dockertest.OK(""),
	})
	adapter := newAdapter(t, builderdocker.Config{RegistryCAFile: dockertest.RegistryCAFile(t)})
	workspace := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "Dockerfile"), []byte("FROM scratch\n"), 0o600))

	containerID, logs, err := adapter.RunBuildContainer(context.Background(), model.BuildRuntimeSpec{
		RegistryUsername: username,
		RegistryPassword: password,
		WorkspaceDir:     workspace,
		DestinationTag:   "registry:5000/owner_image:latest",
		KanikoImage:      "kaniko",
		Dockerfile:       "Dockerfile",
		NetworkName:      "build_net",
	})
	require.NoError(t, err)
	require.Equal(t, "build-container", containerID)
	_, err = io.ReadAll(logs)
	require.NoError(t, err)
	require.NoError(t, logs.Close())

	encodedAuth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	created := daemon.Requests("/containers/create")
	require.Len(t, created, 1)
	var spec struct {
		Env []string
		Cmd []string
	}
	require.NoError(t, json.Unmarshal(created[0].Body, &spec))
	require.NotEmpty(t, spec.Cmd)
	for _, field := range [][]string{spec.Env, spec.Cmd} {
		for _, value := range field {
			require.NotContains(t, value, password)
			require.NotContains(t, value, encodedAuth)
		}
	}

	copies := daemon.Requests("/containers/build-container/archive")
	require.Len(t, copies, 2)
	require.Equal(t, "/workspace", copies[0].Query.Get("path"))
	require.NotContains(t, string(copies[0].Body), password)
	require.NotContains(t, string(copies[0].Body), encodedAuth)
	require.Equal(t, "/", copies[1].Query.Get("path"))
	files := readTar(t, copies[1].Body)
	require.ElementsMatch(t, []string{"kaniko/.docker/config.json", "kaniko/ssl/certs/dcm-registry.crt"}, slices.Collect(maps.Keys(files)))
	for name, file := range files {
		require.Equal(t, int64(0o600), file.mode, name)
	}
	var config struct {
		Auths map[string]struct{ Auth string }
	}
	require.NoError(t, json.Unmarshal(files["kaniko/.docker/config.json"].data, &config))
	require.Equal(t, encodedAuth, config.Auths["registry:5000"].Auth)
	require.Len(t, config.Auths, 1)
}

type tarFile struct {
	mode int64
	data []byte
}

func readTar(t *testing.T, archive []byte) map[string]tarFile {
	t.Helper()
	files := make(map[string]tarFile)
	reader := tar.NewReader(bytes.NewReader(archive))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return files
		}
		require.NoError(t, err)
		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		files[header.Name] = tarFile{mode: header.Mode, data: data}
	}
}

func newAdapter(t *testing.T, cfg builderdocker.Config) *builderdocker.Adapter {
	t.Helper()
	if cfg.InstanceID == "" {
		cfg.InstanceID = "builder-test"
	}
	adapter, err := builderdocker.NewAdapter(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable
	return adapter
}

// Exit code 137 is SIGKILL: the kernel's OOM killer or the platform stopped
// the build container, so the build ran out of the resources it was given.
func TestAdapterWaitForBuildReportsAKilledContainerAsResourceExhaustion(t *testing.T) {
	for _, tt := range []struct {
		name              string
		exitCode          int
		wantErr           bool
		wantResourceError bool
	}{
		{name: "exit 0 is success", exitCode: 0},
		{name: "exit 137 is resource exhaustion", exitCode: 137, wantErr: true, wantResourceError: true},
		{name: "another exit code is a plain failure", exitCode: 1, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			daemon := dockertest.Serve(t, map[string]dockertest.Response{
				"/containers/container-id/wait": dockertest.OK(`{"StatusCode":` + strconv.Itoa(tt.exitCode) + `}`),
			})
			adapter := newAdapter(t, builderdocker.Config{RegistryCAFile: dockertest.RegistryCAFile(t)})

			err := adapter.WaitForBuild(context.Background(), "container-id")

			require.Len(t, daemon.Requests("/containers/container-id/wait"), 1)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Equal(t, tt.wantResourceError, errors.Is(err, apperrors.ErrResourceExhausted))
		})
	}
}

// Several Builders may share a Docker daemon, so each build container names
// the instance that started it.
func TestAdapterRunBuildContainerLabelsItWithTheInstance(t *testing.T) {
	daemon := dockertest.Serve(t, map[string]dockertest.Response{
		"/networks/build_net": dockertest.OK(isolatedBuildNetwork),
		"/images/kaniko/json": dockertest.OK(`{"Id":"kaniko"}`),
		// Failing here ends the run once the container request is recorded.
		"/containers/create": dockertest.Error(http.StatusInternalServerError, "stop after create"),
	})
	adapter := newAdapter(t, builderdocker.Config{RegistryCAFile: dockertest.RegistryCAFile(t), InstanceID: "builder-a"})

	_, _, err := adapter.RunBuildContainer(context.Background(), model.BuildRuntimeSpec{
		BuildID:          "build-id",
		OwnerID:          "owner-id",
		RegistryUsername: "builder",
		RegistryPassword: "password",
		DestinationTag:   "registry:5000/owner_image:latest",
		KanikoImage:      "kaniko",
		Dockerfile:       "Dockerfile",
		NetworkName:      "build_net",
	})
	require.Error(t, err)

	created := daemon.Requests("/containers/create")
	require.Len(t, created, 1)
	var request struct{ Labels map[string]string }
	require.NoError(t, json.Unmarshal(created[0].Body, &request))
	require.Equal(t, map[string]string{
		"managed_by":              "docker-cloud-manager",
		"dcm.resource_type":       "build",
		"dcm.build_id":            "build-id",
		"dcm.owner_id":            "owner-id",
		"dcm.builder_instance_id": "builder-a",
	}, request.Labels)
}

// A starting Builder removes its own leftover build containers and never a
// sibling instance's running build.
func TestAdapterCleanupOrphanBuildContainersRemovesOnlyItsOwnInstances(t *testing.T) {
	daemon := dockertest.Serve(t, map[string]dockertest.Response{
		// The daemon filters by label; the list also carries a sibling's build
		// to prove the adapter does not rely on that filter alone.
		"/containers/json": dockertest.OK(`[
			{"Id":"own-build","Labels":{"managed_by":"docker-cloud-manager","dcm.resource_type":"build","dcm.builder_instance_id":"builder-a"}},
			{"Id":"sibling-build","Labels":{"managed_by":"docker-cloud-manager","dcm.resource_type":"build","dcm.builder_instance_id":"builder-b"}}
		]`),
		"DELETE /containers/own-build": dockertest.NoContent(),
	})
	adapter := newAdapter(t, builderdocker.Config{RegistryCAFile: dockertest.RegistryCAFile(t), InstanceID: "builder-a"})

	require.NoError(t, adapter.CleanupOrphanBuildContainers(context.Background()))

	listed := daemon.Requests("/containers/json")
	require.Len(t, listed, 1)
	var filters map[string]map[string]bool
	require.NoError(t, json.Unmarshal([]byte(listed[0].Query.Get("filters")), &filters))
	require.Equal(t, map[string]bool{
		"managed_by=docker-cloud-manager":   true,
		"dcm.resource_type=build":           true,
		"dcm.builder_instance_id=builder-a": true,
	}, filters["label"])
	require.Len(t, daemon.Requests("/containers/own-build"), 1)
	require.Empty(t, daemon.Requests("/containers/sibling-build"))
}

func TestNewAdapterRequiresAnInstanceID(t *testing.T) {
	_, err := builderdocker.NewAdapter(builderdocker.Config{})
	require.ErrorContains(t, err, "instance ID is required")
}
