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

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/moby/go-archive"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

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
			return nil, errors.New("read registry CA")
		}
		if !x509.NewCertPool().AppendCertsFromPEM(ca) {
			return nil, errors.New("invalid registry CA")
		}
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
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
		_ = a.CleanBuildContainer(context.Background(), resp.ID)
		return "", nil, fmt.Errorf("failed to tar workspace: %w", err)
	}
	defer tarStream.Close()

	err = a.cli.CopyToContainer(ctx, resp.ID, "/workspace", tarStream, container.CopyToContainerOptions{})
	if err != nil {
		_ = a.CleanBuildContainer(context.Background(), resp.ID)
		return "", nil, fmt.Errorf("failed to copy files to kaniko: %w", err)
	}

	// Auth lives under Kaniko's excluded directory, outside the uploaded context.
	authJSON, _ := json.Marshal(map[string]any{"auths": map[string]any{authority: map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte(params.RegistryUsername + ":" + params.RegistryPassword))}}})
	var secrets bytes.Buffer
	tw := tar.NewWriter(&secrets)
	for _, f := range []struct {
		name string
		data []byte
	}{{"kaniko/.docker/config.json", authJSON}, {"kaniko/ssl/certs/dcm-registry.crt", a.registryCA}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0600, Size: int64(len(f.data))}); err != nil {
			_ = a.CleanBuildContainer(context.WithoutCancel(ctx), resp.ID)
			return "", nil, err
		}
		if _, err := tw.Write(f.data); err != nil {
			_ = a.CleanBuildContainer(context.WithoutCancel(ctx), resp.ID)
			return "", nil, err
		}
	}
	if err := tw.Close(); err != nil {
		_ = a.CleanBuildContainer(context.WithoutCancel(ctx), resp.ID)
		return "", nil, err
	}
	if err := a.cli.CopyToContainer(ctx, resp.ID, "/", &secrets, container.CopyToContainerOptions{}); err != nil {
		_ = a.CleanBuildContainer(context.WithoutCancel(ctx), resp.ID)
		return "", nil, errors.New("install scoped build registry credentials")
	}

	// 3. Запускаем сборку
	err = a.cli.ContainerStart(ctx, resp.ID, container.StartOptions{})
	if err != nil {
		_ = a.CleanBuildContainer(context.Background(), resp.ID)
		return "", nil, err
	}

	// 4. Получаем сырой поток логов
	rawLogs, err := a.cli.ContainerLogs(ctx, resp.ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		_ = a.CleanBuildContainer(context.Background(), resp.ID)
		return "", nil, err
	}

	// 5. Очищаем Docker-заголовки из логов с помощью stdcopy
	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer pw.Close()
		defer rawLogs.Close()
		// StdCopy разделяет stdout и stderr, мы направляем оба потока в наш Pipe
		if _, err := stdcopy.StdCopy(pw, pw, rawLogs); err != nil && ctx.Err() != nil {
			_ = pw.CloseWithError(ctx.Err())
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = rawLogs.Close()
			_ = pw.CloseWithError(ctx.Err())
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
		return apperrors.ErrUnavailable
	}
	if !nw.Internal || nw.EnableIPv6 || nw.Options["com.docker.network.bridge.name"] != "br-dcm-build" {
		return apperrors.ErrUnavailable
	}
	containers, err := a.cli.ContainerList(ctx, container.ListOptions{Filters: filters.NewArgs(filters.Arg("label", "com.docker.compose.service=build-network-policy"))})
	if err != nil {
		return apperrors.ErrUnavailable
	}
	for _, policy := range containers {
		state, err := a.cli.ContainerInspect(ctx, policy.ID)
		if err == nil && state.State != nil && state.State.Running && state.State.Health != nil && state.State.Health.Status == "healthy" {
			return nil
		}
	}
	return apperrors.ErrUnavailable
}
