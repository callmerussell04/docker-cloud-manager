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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/moby/go-archive"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/platform/apperrors"
)

const buildNetworkPolicyUnavailableMessage = "build network policy is unavailable"

type Adapter struct {
	cli                  *client.Client
	registryCA           []byte
	requireNetworkPolicy bool
}

type Config struct {
	RegistryCAFile       string
	RequireNetworkPolicy bool
}

func NewAdapter(cfg Config) (*Adapter, error) {
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
	return &Adapter{cli: cli, registryCA: ca, requireNetworkPolicy: cfg.RequireNetworkPolicy}, nil
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
	for k, v := range params.BuildArgs {
		cmd = append(cmd, fmt.Sprintf("--build-arg=%s=%s", k, v))
	}

	networkName := params.NetworkName

	// 2. Настраиваем контейнер Kaniko
	pidsLimit := params.PidsLimit
	memorySwap := int64(float64(params.MemoryBytes) * params.MemorySwapMultiplier)
	resp, err := a.cli.ContainerCreate(ctx, &container.Config{
		Image: kanikoImage,
		Env:   []string{"HTTP_PROXY=http://build-egress:3128", "HTTPS_PROXY=http://build-egress:3128", "NO_PROXY=" + authority},
		Cmd:   cmd,
		Labels: map[string]string{
			"managed_by":        "docker-cloud-manager",
			"dcm.resource_type": "build",
			"dcm.build_id":      params.BuildID,
			"dcm.owner_id":      params.OwnerID,
		},
	}, &container.HostConfig{
		SecurityOpt: []string{"no-new-privileges:true"},
		CapDrop:     []string{"NET_RAW"},
		Resources: container.Resources{
			Memory:     params.MemoryBytes,
			MemorySwap: memorySwap,
			CPUQuota:   params.CPUQuota,
			CPUPeriod:  params.CPUPeriod,
			PidsLimit:  &pidsLimit,
		},
	}, &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			networkName: {},
		},
	}, nil, "")

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

func (a *Adapter) CleanupOrphanBuildContainers(ctx context.Context) error {
	containers, err := a.cli.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", "managed_by=docker-cloud-manager"),
			filters.Arg("label", "dcm.resource_type=build"),
		),
	})
	if err != nil {
		return err
	}
	for _, c := range containers {
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
