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

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/testenv"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Rules touch only unique disposable bridge/chain names, with explicit cleanup.
// No production network or firewall chain owned by DCM is modified. The same
// policy container also isolates the proxy bridge: exposed containers reach
// each other only through Traefik.
func TestNetworkPolicyIsolatesBuildAndProxyBridges(t *testing.T) {
	// Manual deployment check (docs/deployment-single-vm.md); testenv exempts it.
	testenv.Enabled(t, "CORE_TEST_NETWORK_POLICY")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	bridge, egressBridge, proxyBridge := "br-rt-"+suffix, "br-re-"+suffix, "br-rp-"+suffix
	buildChain, egressChain, proxyChain := "RTB-"+suffix, "RTE-"+suffix, "RTP-"+suffix
	nwID, gateway := createFixedBridge(ctx, t, cli, "registry-net-test-"+suffix, bridge, true)
	prefix := gateway[:strings.LastIndex(gateway, ".")+1]
	edgeIP, proxyIP := prefix+"2", prefix+"3"
	// The proxy bridge is not internal, as in production: exposed services keep
	// their outbound access, only same-bridge traffic is restricted.
	proxyNetID, proxyGateway := createFixedBridge(ctx, t, cli, "proxy-net-test-"+suffix, proxyBridge, false)
	proxyPrefix := proxyGateway[:strings.LastIndex(proxyGateway, ".")+1]
	traefikIP, exposedA, exposedB, exposedProbe := proxyPrefix+"2", proxyPrefix+"10", proxyPrefix+"11", proxyPrefix+"12"
	hostListener, err := net.Listen("tcp", net.JoinHostPort(gateway, "0"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = hostListener.Close() })
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../.."))
	policyImage := "dcm-registry-policy-test:" + suffix
	buildContext := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(buildContext, "deployment/registry"), 0700))
	for _, name := range []string{"network-policy.sh", "network-policy-check.sh", "network-policy.Dockerfile"} {
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
 "$tool" -w -D "$forward" -i %[1]s -j %[2]s 2>/dev/null || true
 "$tool" -w -D "$forward" -i %[3]s -j %[4]s 2>/dev/null || true
 "$tool" -w -D "$forward" -i %[5]s -o %[5]s -j %[6]s 2>/dev/null || true
 "$tool" -w -D INPUT -i %[1]s -j DROP 2>/dev/null || true
 "$tool" -w -D INPUT -i %[3]s -j DROP 2>/dev/null || true
 for chain in %[2]s %[4]s %[6]s; do "$tool" -w -F "$chain" 2>/dev/null || true; "$tool" -w -X "$chain" 2>/dev/null || true; done
done`, bridge, buildChain, egressBridge, egressChain, proxyBridge, proxyChain)
		command := exec.Command("docker", "run", "--rm", "--network", "host", "--cap-add", "NET_ADMIN", "--entrypoint", "sh", policyImage, "-c", cleanup)
		result, err := command.CombinedOutput()
		if err != nil {
			t.Errorf("remove isolated policy rules: %v %s", err, result)
		}
	})
	policy, err := cli.ContainerCreate(ctx, &container.Config{Image: policyImage, Env: []string{"BUILD_BRIDGE=" + bridge, "EGRESS_BRIDGE=" + egressBridge, "PROXY_BRIDGE=" + proxyBridge, "BUILD_CHAIN=" + buildChain, "EGRESS_CHAIN=" + egressChain, "PROXY_CHAIN=" + proxyChain, "REGISTRY_EDGE_IP=" + edgeIP, "BUILD_PROXY_IP=" + proxyIP, "TRAEFIK_PROXY_IP=" + traefikIP}}, &container.HostConfig{NetworkMode: "host", CapAdd: []string{"NET_ADMIN"}}, nil, nil, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.ContainerRemove(context.Background(), policy.ID, container.RemoveOptions{Force: true}) })
	require.NoError(t, cli.ContainerStart(ctx, policy.ID, container.StartOptions{}))
	require.Eventually(t, func() bool {
		inspection, err := cli.ContainerInspect(ctx, policy.ID)
		if err != nil || !inspection.State.Running {
			return false
		}
		execID, err := cli.ContainerExecCreate(ctx, policy.ID, container.ExecOptions{Cmd: []string{"sh", "/usr/local/bin/network-policy-check"}})
		if err != nil {
			return false
		}
		if err := cli.ContainerExecStart(ctx, execID.ID, container.ExecStartOptions{}); err != nil {
			return false
		}
		done, err := cli.ContainerExecInspect(ctx, execID.ID)
		return err == nil && done.ExitCode == 0
	}, 10*time.Second, 200*time.Millisecond)
	createListener := func(networkID, ip, commands string) {
		response, err := cli.ContainerCreate(ctx, &container.Config{Image: "busybox:latest", Cmd: []string{"sh", "-c", commands}}, nil, &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{networkID: {IPAMConfig: &network.EndpointIPAMConfig{IPv4Address: ip}}}}, nil, "")
		require.NoError(t, err)
		t.Cleanup(func() {
			_ = cli.ContainerRemove(context.Background(), response.ID, container.RemoveOptions{Force: true})
		})
		require.NoError(t, cli.ContainerStart(ctx, response.ID, container.StartOptions{}))
	}
	createListener(nwID, edgeIP, "httpd -p 5000; httpd -p 50052; sleep 100")
	createListener(nwID, proxyIP, "httpd -p 3128; sleep 100")
	createListener(proxyNetID, exposedA, "httpd -p 8080; sleep 100")
	createListener(proxyNetID, exposedB, "httpd -p 8080; sleep 100")
	probePath := filepath.Join(t.TempDir(), "probe")
	compile := exec.CommandContext(ctx, "go", "build", "-o", probePath, filepath.Join(root, "backend/tests/integration/registry_isolation/testdata/registry-probe/main.go"))
	compile.Env = append(os.Environ(), "CGO_ENABLED=0", "GOCACHE=/tmp/go-build-cache")
	output, err = compile.CombinedOutput()
	require.NoError(t, err, "compile probe: %s", output)
	probeBinary, err := os.ReadFile(probePath)
	require.NoError(t, err)
	runNetworkProbe(ctx, t, cli, probeBinary, nwID, "", "allow="+edgeIP+":5000", "allow="+proxyIP+":3128", "deny="+edgeIP+":50052", "deny="+hostListener.Addr().String(), "deny=169.254.169.254:80", "deny=1.1.1.1:443")
	// Another exposed container reaches neither exposed service.
	runNetworkProbe(ctx, t, cli, probeBinary, proxyNetID, exposedProbe, "deny="+exposedA+":8080", "deny="+exposedB+":8080")
	// Traefik, at its fixed address, reaches both.
	runNetworkProbe(ctx, t, cli, probeBinary, proxyNetID, traefikIP, "allow="+exposedA+":8080", "allow="+exposedB+":8080")
}

// createFixedBridge creates a bridge with the given device name and explicit
// IPAM, so fixture endpoints can request fixed addresses. It returns the
// network ID and its gateway address.
func createFixedBridge(ctx context.Context, t *testing.T, cli *client.Client, name, bridge string, internal bool) (string, string) {
	t.Helper()
	options := network.CreateOptions{Internal: internal, Driver: "bridge", Options: map[string]string{"com.docker.network.bridge.name": bridge}}
	nw, err := cli.NetworkCreate(ctx, name, options)
	require.NoError(t, err)
	info, err := cli.NetworkInspect(ctx, nw.ID, network.InspectOptions{})
	require.NoError(t, err)
	// Docker allocates a free subnet first; recreate with that subnet explicit.
	require.NoError(t, cli.NetworkRemove(ctx, nw.ID))
	options.IPAM = &network.IPAM{Config: info.IPAM.Config}
	nw, err = cli.NetworkCreate(ctx, name, options)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.NetworkRemove(context.Background(), nw.ID) })
	return nw.ID, info.IPAM.Config[0].Gateway
}

// runNetworkProbe runs the probe's network mode in a container on networkID,
// at ip when it is not empty, and requires every allow/deny expectation to hold.
func runNetworkProbe(ctx context.Context, t *testing.T, cli *client.Client, probeBinary []byte, networkID, ip string, expectations ...string) {
	t.Helper()
	endpoint := &network.EndpointSettings{}
	if ip != "" {
		endpoint.IPAMConfig = &network.EndpointIPAMConfig{IPv4Address: ip}
	}
	cmd := append([]string{"/tmp/probe", "network"}, expectations...)
	probe, err := cli.ContainerCreate(ctx, &container.Config{Image: "busybox:latest", Cmd: cmd}, &container.HostConfig{CapDrop: []string{"NET_RAW"}}, &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{networkID: endpoint}}, nil, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.ContainerRemove(context.Background(), probe.ID, container.RemoveOptions{Force: true}) })
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "probe", Mode: 0700, Size: int64(len(probeBinary))}))
	_, err = tw.Write(probeBinary)
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
