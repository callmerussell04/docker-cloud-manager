package registry_isolation_test

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Rules touch only unique disposable bridge/chain names, with explicit cleanup.
// No production network or firewall chain owned by DCM is modified.
func TestBuildNetworkPolicyDeniesControlPlaneAndHost(t *testing.T) {
	if os.Getenv("CORE_TEST_NETWORK_POLICY") != "1" {
		t.Skip("manual deployment check (docs/deployment-single-vm.md): set CORE_TEST_NETWORK_POLICY=1 for isolated host firewall verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	bridge, egressBridge := "br-rt-"+suffix, "br-re-"+suffix
	buildChain, egressChain := "RTB-"+suffix, "RTE-"+suffix
	nw, err := cli.NetworkCreate(ctx, "registry-net-test-"+suffix, network.CreateOptions{Internal: true, Driver: "bridge", Options: map[string]string{"com.docker.network.bridge.name": bridge}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.NetworkRemove(context.Background(), nw.ID) })
	info, err := cli.NetworkInspect(ctx, nw.ID, network.InspectOptions{})
	require.NoError(t, err)
	// Docker allocates a free subnet first. Recreate it with explicit IPAM so
	// fixture endpoints can request the fixed edge/proxy addresses.
	require.NoError(t, cli.NetworkRemove(ctx, nw.ID))
	nw, err = cli.NetworkCreate(ctx, "registry-net-test-"+suffix, network.CreateOptions{Internal: true, Driver: "bridge", Options: map[string]string{"com.docker.network.bridge.name": bridge}, IPAM: &network.IPAM{Config: info.IPAM.Config}})
	require.NoError(t, err)
	gateway := info.IPAM.Config[0].Gateway
	prefix := gateway[:strings.LastIndex(gateway, ".")+1]
	edgeIP, proxyIP := prefix+"2", prefix+"3"
	hostListener, err := net.Listen("tcp", net.JoinHostPort(gateway, "0"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = hostListener.Close() })
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../.."))
	policyImage := "dcm-registry-policy-test:" + suffix
	buildContext := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(buildContext, "deployment/registry"), 0700))
	for _, name := range []string{"network-policy.sh", "network-policy-check.sh", "network-policy.Dockerfile", "network-policy-packages.lock"} {
		data, err := os.ReadFile(filepath.Join(root, "deployment/registry", name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(buildContext, "deployment/registry", name), data, 0600))
	}
	build := exec.CommandContext(ctx, "docker", "build", "-q", "-t", policyImage, "-f", filepath.Join(buildContext, "deployment/registry/network-policy.Dockerfile"), buildContext)
	output, err := build.CombinedOutput()
	require.NoError(t, err, "build policy: %s", output)
	t.Cleanup(func() { _, _ = cli.ImageRemove(context.Background(), policyImage, image.RemoveOptions{Force: true}) })
	// Cleanup runs even if policy setup fails after installing one address family.
	t.Cleanup(func() {
		cleanup := fmt.Sprintf(`for tool in iptables ip6tables; do
 forward=FORWARD; [ "$tool" != iptables ] || forward=DOCKER-USER
 "$tool" -w -D "$forward" -i %s -j %s 2>/dev/null || true
 "$tool" -w -D "$forward" -i %s -j %s 2>/dev/null || true
 "$tool" -w -D INPUT -i %s -j DROP 2>/dev/null || true
 "$tool" -w -D INPUT -i %s -j DROP 2>/dev/null || true
 "$tool" -w -F %s 2>/dev/null || true; "$tool" -w -X %s 2>/dev/null || true
 "$tool" -w -F %s 2>/dev/null || true; "$tool" -w -X %s 2>/dev/null || true
done`, bridge, buildChain, egressBridge, egressChain, bridge, egressBridge, buildChain, buildChain, egressChain, egressChain)
		command := exec.Command("docker", "run", "--rm", "--network", "host", "--cap-add", "NET_ADMIN", "--entrypoint", "sh", policyImage, "-c", cleanup)
		result, err := command.CombinedOutput()
		if err != nil {
			t.Errorf("remove isolated policy rules: %v %s", err, result)
		}
	})
	policy, err := cli.ContainerCreate(ctx, &container.Config{Image: policyImage, Env: []string{"BUILD_BRIDGE=" + bridge, "EGRESS_BRIDGE=" + egressBridge, "BUILD_CHAIN=" + buildChain, "EGRESS_CHAIN=" + egressChain, "REGISTRY_EDGE_IP=" + edgeIP, "BUILD_PROXY_IP=" + proxyIP}}, &container.HostConfig{NetworkMode: "host", CapAdd: []string{"NET_ADMIN"}}, nil, nil, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.ContainerRemove(context.Background(), policy.ID, container.RemoveOptions{Force: true}) })
	require.NoError(t, cli.ContainerStart(ctx, policy.ID, container.StartOptions{}))
	require.Eventually(t, func() bool {
		inspection, err := cli.ContainerInspect(ctx, policy.ID)
		if err != nil || !inspection.State.Running {
			return false
		}
		execID, err := cli.ContainerExecCreate(ctx, policy.ID, container.ExecOptions{Cmd: []string{"test", "-f", "/run/policy-ready"}})
		if err != nil {
			return false
		}
		if err := cli.ContainerExecStart(ctx, execID.ID, container.ExecStartOptions{}); err != nil {
			return false
		}
		done, err := cli.ContainerExecInspect(ctx, execID.ID)
		return err == nil && done.ExitCode == 0
	}, 10*time.Second, 200*time.Millisecond)
	createListener := func(ip, commands string) {
		response, err := cli.ContainerCreate(ctx, &container.Config{Image: "busybox:latest", Cmd: []string{"sh", "-c", commands}}, nil, &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{nw.ID: {IPAMConfig: &network.EndpointIPAMConfig{IPv4Address: ip}}}}, nil, "")
		require.NoError(t, err)
		t.Cleanup(func() {
			_ = cli.ContainerRemove(context.Background(), response.ID, container.RemoveOptions{Force: true})
		})
		require.NoError(t, cli.ContainerStart(ctx, response.ID, container.StartOptions{}))
	}
	createListener(edgeIP, "httpd -p 5000; httpd -p 50052; sleep 100")
	createListener(proxyIP, "httpd -p 3128; sleep 100")
	probePath := filepath.Join(t.TempDir(), "probe")
	compile := exec.CommandContext(ctx, "go", "build", "-o", probePath, filepath.Join(root, "backend/tests/testdata/registry-probe/main.go"))
	compile.Env = append(os.Environ(), "CGO_ENABLED=0", "GOCACHE=/tmp/go-build-cache")
	output, err = compile.CombinedOutput()
	require.NoError(t, err, "compile probe: %s", output)
	probe, err := cli.ContainerCreate(ctx, &container.Config{Image: "busybox:latest", Cmd: []string{"/tmp/probe", "network", "allow=" + edgeIP + ":5000", "allow=" + proxyIP + ":3128", "deny=" + edgeIP + ":50052", "deny=" + hostListener.Addr().String(), "deny=169.254.169.254:80", "deny=1.1.1.1:443"}}, &container.HostConfig{CapDrop: []string{"NET_RAW"}}, &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{nw.ID: {}}}, nil, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.ContainerRemove(context.Background(), probe.ID, container.RemoveOptions{Force: true}) })
	data, err := os.ReadFile(probePath)
	require.NoError(t, err)
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "probe", Mode: 0700, Size: int64(len(data))}))
	_, err = tw.Write(data)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, cli.CopyToContainer(ctx, probe.ID, "/tmp", &archive, container.CopyToContainerOptions{}))
	require.NoError(t, cli.ContainerStart(ctx, probe.ID, container.StartOptions{}))
	statuses, errs := cli.ContainerWait(ctx, probe.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errs:
		require.NoError(t, err)
	case status := <-statuses:
		logs, err := cli.ContainerLogs(ctx, probe.ID, container.LogsOptions{ShowStdout: true, ShowStderr: true})
		require.NoError(t, err)
		output, _ := io.ReadAll(logs)
		_ = logs.Close() // fully read log stream; close failure is not actionable
		require.EqualValues(t, 0, status.StatusCode, "network probe: %s", output)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
