package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/moby/go-archive"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
)

const (
	buildNetworkPolicyUnavailableMessage = "build network policy is unavailable"
	buildEgressProxyURL                  = "http://build-egress:3128"
	defaultManagedBy                     = "docker-cloud-manager"
	// instanceLabel names the Builder instance that started a build
	// container. Core lists build containers by the other labels only.
	instanceLabel = "dcm.builder_instance_id"
)

type Adapter struct {
	cli                  *client.Client
	registryCA           []byte
	requireNetworkPolicy bool
	managedBy            string
	instanceID           string
	cgroupParent         string
	logger               *slog.Logger
	// rootfsSizeRefused records that Docker refused a root filesystem size
	// cap, so it is not asked for again in this process.
	rootfsSizeRefused atomic.Bool
}

type Config struct {
	RegistryCAFile       string
	RequireNetworkPolicy bool
	// ManagedBy is the managed_by label value the adapter gives build
	// containers and cleans up orphans by. Empty selects docker-cloud-manager.
	ManagedBy string
	// InstanceID is the Builder instance's stable identity. Build containers
	// carry it as a label, and orphan cleanup removes only containers carrying
	// it. Required.
	InstanceID string
	// CgroupParent is the cgroup Docker places build containers under. Empty
	// keeps Docker's default parent.
	CgroupParent string
	// Logger receives the adapter's warnings. Nil selects slog.Default.
	Logger *slog.Logger
}

func NewAdapter(cfg Config) (*Adapter, error) {
	if cfg.InstanceID == "" {
		return nil, errors.New("builder docker adapter: instance ID is required")
	}
	var ca []byte
	if cfg.RegistryCAFile != "" {
		var err error
		ca, err = os.ReadFile(cfg.RegistryCAFile)
		if err != nil {
			return nil, fmt.Errorf("read registry CA: %w", err)
		}
		if !x509.NewCertPool().AppendCertsFromPEM(ca) {
			return nil, errors.New("invalid registry CA")
		}
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}
	managedBy := cfg.ManagedBy
	if managedBy == "" {
		managedBy = defaultManagedBy
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Adapter{cli: cli, registryCA: ca, requireNetworkPolicy: cfg.RequireNetworkPolicy, managedBy: managedBy, instanceID: cfg.InstanceID, cgroupParent: strings.TrimSpace(cfg.CgroupParent), logger: logger}, nil
}

func (a *Adapter) RunBuildContainer(ctx context.Context, params model.BuildRuntimeSpec) (string, io.ReadCloser, error) {
	if err := a.ValidateBuildNetwork(ctx, params.NetworkName); err != nil {
		return "", nil, err
	}

	if params.RegistryUsername == "" || params.RegistryPassword == "" || len(a.registryCA) == 0 {
		return "", nil, errors.New("build registry authority is missing")
	}
	authority, _, ok := strings.Cut(params.DestinationTag, "/")
	if !ok {
		return "", nil, errors.New("invalid build destination")
	}
	// Auth lives under Kaniko's excluded directory, outside the uploaded context.
	authJSON, err := json.Marshal(map[string]any{"auths": map[string]any{authority: map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte(params.RegistryUsername + ":" + params.RegistryPassword))}}})
	if err != nil {
		return "", nil, fmt.Errorf("encode build registry credentials: %w", err)
	}

	// 1. Убеждаемся, что образ Kaniko есть на хосте
	kanikoImage := params.KanikoImage
	if _, err := a.cli.ImageInspect(ctx, kanikoImage); err != nil {
		pullStream, err := a.cli.ImagePull(ctx, kanikoImage, image.PullOptions{})
		if err != nil {
			return "", nil, err
		}
		_, err = io.Copy(io.Discard, pullStream)
		closeErr := pullStream.Close()
		if err != nil {
			return "", nil, err
		}
		if closeErr != nil {
			return "", nil, closeErr
		}
	}

	kanikoContext := "dir:///workspace"
	if params.ContextSubDir != "" && params.ContextSubDir != "." {
		kanikoContext = "dir:///workspace/" + params.ContextSubDir
	}
	kanikoDockerfile := filepath.Join("/workspace", params.ContextSubDir, params.Dockerfile)

	cmd := []string{
		"--context=" + kanikoContext,
		"--dockerfile=" + kanikoDockerfile,
		"--destination=" + params.DestinationTag,
		"--cache=false",
		"--registry-certificate=" + authority + "=/kaniko/ssl/certs/dcm-registry.crt",
		"--push-retry=2",
		"--image-fs-extract-retry=2",
		"--ignore-path=/workspace",
	}

	// Добавляем Build Args для Kaniko
	buildArgs := kanikoBuildArgs(params.BuildArgs, authority)
	for _, k := range slices.Sorted(maps.Keys(buildArgs)) {
		cmd = append(cmd, fmt.Sprintf("--build-arg=%s=%s", k, buildArgs[k]))
	}

	networkName := params.NetworkName

	// 2. Настраиваем контейнер Kaniko
	pidsLimit := params.PidsLimit
	memorySwap := int64(float64(params.MemoryBytes) * params.MemorySwapMultiplier)
	containerConfig := &container.Config{
		Image: kanikoImage,
		Env:   []string{"HTTP_PROXY=" + buildEgressProxyURL, "HTTPS_PROXY=" + buildEgressProxyURL, "NO_PROXY=" + authority},
		Cmd:   cmd,
		Labels: map[string]string{
			"managed_by":        a.managedBy,
			"dcm.resource_type": "build",
			"dcm.build_id":      params.BuildID,
			"dcm.owner_id":      params.OwnerID,
			instanceLabel:       a.instanceID,
		},
	}
	hostConfig := &container.HostConfig{
		SecurityOpt: []string{"no-new-privileges:true"},
		CapDrop:     []string{"NET_RAW"},
		Resources: container.Resources{
			Memory:       params.MemoryBytes,
			CgroupParent: a.cgroupParent,
			MemorySwap:   memorySwap,
			CPUQuota:     params.CPUQuota,
			CPUPeriod:    params.CPUPeriod,
			PidsLimit:    &pidsLimit,
		},
	}
	networkingConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			networkName: {},
		},
	}
	resp, err := a.createBuildContainer(ctx, containerConfig, hostConfig, networkingConfig, params.MaxDiskBytes)
	if err != nil {
		return "", nil, err
	}

	// 2. Копируем исходный код прямо внутрь созданного контейнера в /workspace
	// Keep this tar uncompressed and copy before starting Kaniko. Uploaded archives
	// are extracted locally, never forwarded to the daemon's decompression path.
	tarStream, err := archive.TarWithOptions(params.WorkspaceDir, &archive.TarOptions{})
	if err != nil {
		a.removeFailedBuildContainer(ctx, resp.ID)
		return "", nil, fmt.Errorf("failed to tar workspace: %w", err)
	}
	defer func() { _ = tarStream.Close() }() // read-only stream; close failure is not actionable

	err = a.cli.CopyToContainer(ctx, resp.ID, "/workspace", tarStream, container.CopyToContainerOptions{})
	if err != nil {
		a.removeFailedBuildContainer(ctx, resp.ID)
		return "", nil, fmt.Errorf("failed to copy files to kaniko: %w", err)
	}

	var secrets bytes.Buffer
	tw := tar.NewWriter(&secrets)
	for _, f := range []struct {
		name string
		data []byte
	}{{"kaniko/.docker/config.json", authJSON}, {"kaniko/ssl/certs/dcm-registry.crt", a.registryCA}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0600, Size: int64(len(f.data))}); err != nil {
			a.removeFailedBuildContainer(ctx, resp.ID)
			return "", nil, fmt.Errorf("archive scoped build registry credentials: %w", err)
		}
		if _, err := tw.Write(f.data); err != nil {
			a.removeFailedBuildContainer(ctx, resp.ID)
			return "", nil, fmt.Errorf("archive scoped build registry credentials: %w", err)
		}
	}
	if err := tw.Close(); err != nil {
		a.removeFailedBuildContainer(ctx, resp.ID)
		return "", nil, fmt.Errorf("archive scoped build registry credentials: %w", err)
	}
	if err := a.cli.CopyToContainer(ctx, resp.ID, "/", &secrets, container.CopyToContainerOptions{}); err != nil {
		a.removeFailedBuildContainer(ctx, resp.ID)
		return "", nil, fmt.Errorf("install scoped build registry credentials: %w", err)
	}

	// 3. Запускаем сборку
	err = a.cli.ContainerStart(ctx, resp.ID, container.StartOptions{})
	if err != nil {
		a.removeFailedBuildContainer(ctx, resp.ID)
		return "", nil, fmt.Errorf("start build container: %w", err)
	}

	// 4. Получаем сырой поток логов
	rawLogs, err := a.cli.ContainerLogs(ctx, resp.ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		a.removeFailedBuildContainer(ctx, resp.ID)
		return "", nil, fmt.Errorf("stream build container logs: %w", err)
	}

	// 5. Очищаем Docker-заголовки из логов с помощью stdcopy
	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		// StdCopy разделяет stdout и stderr, мы направляем оба потока в наш Pipe
		_, err := stdcopy.StdCopy(pw, pw, rawLogs)
		_ = rawLogs.Close() // read-only stream; close failure is not actionable
		if err != nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		// A nil error closes with io.EOF; a demux failure reaches the log reader
		// so the build ends as failed_internal instead of with truncated logs.
		_ = pw.CloseWithError(err) // PipeWriter.CloseWithError always returns nil
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = rawLogs.Close()              // read-only stream; closing unblocks the demux copy
			_ = pw.CloseWithError(ctx.Err()) // PipeWriter.CloseWithError always returns nil
		case <-done:
		}
	}()

	return resp.ID, pr, nil
}

// createBuildContainer creates the build container with its root filesystem
// capped at maxDiskBytes, unless the cap is zero or Docker has refused it
// before. Docker refuses it where the storage driver cannot enforce it: as an
// invalid parameter, or as Docker 28's overlay2 driver does without xfs
// project quotas, as a server error naming --storage-opt. The container is
// then created without the cap. Only when that create succeeds was the cap
// what Docker refused: one warning is logged and the cap is not asked for
// again in this process; Builder's disk watch caps the build there. When it
// fails too, the failure was another problem, which is returned, and the cap
// is still asked for next time.
func (a *Adapter) createBuildContainer(ctx context.Context, cfg *container.Config, hostConfig *container.HostConfig, networking *network.NetworkingConfig, maxDiskBytes int64) (container.CreateResponse, error) {
	if maxDiskBytes <= 0 || a.rootfsSizeRefused.Load() {
		return a.cli.ContainerCreate(ctx, cfg, hostConfig, networking, nil, "")
	}
	capped := *hostConfig
	capped.StorageOpt = map[string]string{"size": strconv.FormatInt(maxDiskBytes, 10)}
	resp, err := a.cli.ContainerCreate(ctx, cfg, &capped, networking, nil, "")
	if err == nil || !isRootfsSizeRefusal(err) {
		return resp, err
	}
	resp, retryErr := a.cli.ContainerCreate(ctx, cfg, hostConfig, networking, nil, "")
	if retryErr != nil {
		return resp, retryErr
	}
	if a.rootfsSizeRefused.CompareAndSwap(false, true) {
		a.logger.WarnContext(ctx, "docker cannot cap the build container root filesystem; build disk is capped by the disk watch only", "error", err)
	}
	return resp, nil
}

// isRootfsSizeRefusal reports whether a container create failed in a way the
// daemon uses to refuse the root filesystem size option.
func isRootfsSizeRefusal(err error) bool {
	return cerrdefs.IsInvalidArgument(err) || strings.Contains(err.Error(), "storage-opt")
}

// BuildDiskUsage reads the size of the build container's writable layer,
// which Docker computes only when asked for it.
func (a *Adapter) BuildDiskUsage(ctx context.Context, containerID string) (int64, error) {
	info, _, err := a.cli.ContainerInspectWithRaw(ctx, containerID, true)
	if err != nil {
		return 0, err
	}
	if info.SizeRw == nil {
		return 0, errors.New("docker reported no writable layer size for the build container")
	}
	return *info.SizeRw, nil
}

// KillBuildContainer sends SIGKILL, so the container exits with
// killedExitCode as after a kernel OOM kill.
func (a *Adapter) KillBuildContainer(ctx context.Context, containerID string) error {
	return a.cli.ContainerKill(ctx, containerID, "SIGKILL")
}

// killedExitCode is a container's exit code after SIGKILL: the kernel's OOM
// killer or the platform stopped it.
const killedExitCode = 137

func (a *Adapter) WaitForBuild(ctx context.Context, containerID string) error {
	statusCh, errCh := a.cli.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	case resp := <-statusCh:
		if resp.Error != nil {
			return errors.New(resp.Error.Message)
		}
		// Код 0 означает, что Kaniko успешно завершил сборку
		if resp.StatusCode == killedExitCode {
			return fmt.Errorf("build container was killed: %w", apperrors.ErrResourceExhausted)
		}
		if resp.StatusCode != 0 {
			return errors.New("build container exited with non-zero status")
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// failedBuildCleanupTimeout bounds removal of a container whose start failed.
const failedBuildCleanupTimeout = 30 * time.Second

// removeFailedBuildContainer removes a container that never produced a build.
// It runs detached from ctx, which may already be canceled, and is bounded.
// The start error is the one reported; a container left behind is removed by
// CleanupOrphanBuildContainers at the next Builder startup.
func (a *Adapter) removeFailedBuildContainer(ctx context.Context, containerID string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), failedBuildCleanupTimeout)
	defer cancel()
	_ = a.CleanBuildContainer(cleanupCtx, containerID) // best-effort rollback; orphan cleanup retries at startup
}

func (a *Adapter) CleanBuildContainer(ctx context.Context, containerID string) error {
	return a.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force: true,
	})
}

// CleanupOrphanBuildContainers removes the build containers this instance left
// behind. Another instance's builds may be running on the same daemon, so it
// removes only containers labelled with this instance's ID, and rechecks the
// label rather than trusting the daemon's filter alone.
func (a *Adapter) CleanupOrphanBuildContainers(ctx context.Context) error {
	containers, err := a.cli.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", "managed_by="+a.managedBy),
			filters.Arg("label", "dcm.resource_type=build"),
			filters.Arg("label", instanceLabel+"="+a.instanceID),
		),
	})
	if err != nil {
		return err
	}
	for _, c := range containers {
		if c.Labels[instanceLabel] != a.instanceID {
			continue
		}
		if err := a.CleanBuildContainer(ctx, c.ID); err != nil {
			return err
		}
	}
	return nil
}

func (a *Adapter) Close() error {
	if a.cli != nil {
		return a.cli.Close()
	}
	return nil
}

// Admission is repeated at execution so daemon restarts cannot start builds
// before the host forwarding and input policy has become healthy again.
func (a *Adapter) ValidateBuildNetwork(ctx context.Context, networkName string) error {
	if !a.requireNetworkPolicy {
		return nil
	}
	nw, err := a.cli.NetworkInspect(ctx, networkName, network.InspectOptions{})
	if err != nil {
		return apperrors.Wrap(apperrors.ErrUnavailable, "build network inspection failed", fmt.Errorf("inspect build network: %w", err))
	}
	if !nw.Internal || nw.EnableIPv6 || nw.Options["com.docker.network.bridge.name"] != "br-dcm-build" {
		return apperrors.New(apperrors.ErrUnavailable, "build network is not isolated")
	}
	containers, err := a.cli.ContainerList(ctx, container.ListOptions{Filters: filters.NewArgs(filters.Arg("label", "com.docker.compose.service=build-network-policy"))})
	if err != nil {
		return apperrors.Wrap(apperrors.ErrUnavailable, buildNetworkPolicyUnavailableMessage, fmt.Errorf("list build network policy containers: %w", err))
	}
	var inspectErrs []error
	for _, policy := range containers {
		state, err := a.cli.ContainerInspect(ctx, policy.ID)
		if err != nil {
			inspectErrs = append(inspectErrs, fmt.Errorf("inspect build network policy container: %w", err))
			continue
		}
		if state.State != nil && state.State.Running && state.State.Health != nil && state.State.Health.Status == "healthy" {
			return nil
		}
	}
	if len(inspectErrs) > 0 {
		return apperrors.Wrap(apperrors.ErrUnavailable, buildNetworkPolicyUnavailableMessage, errors.Join(inspectErrs...))
	}
	return apperrors.New(apperrors.ErrUnavailable, buildNetworkPolicyUnavailableMessage)
}

// kanikoBuildArgs adds the egress proxy to the user's build args. Kaniko's own
// environment does not reach RUN steps, and the build network has no other
// route out, so the proxy settings replace user values of the same name. Both
// spellings are set because tools such as curl read only the lowercase one.
func kanikoBuildArgs(userArgs map[string]string, registryAuthority string) map[string]string {
	args := make(map[string]string, len(userArgs)+6)
	maps.Copy(args, userArgs)
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy"} {
		args[name] = buildEgressProxyURL
	}
	args["NO_PROXY"] = registryAuthority
	args["no_proxy"] = registryAuthority
	return args
}
