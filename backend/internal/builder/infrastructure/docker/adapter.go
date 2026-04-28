package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/moby/go-archive"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/model"
)

type Adapter struct {
	cli *client.Client
}

func NewAdapter() (*Adapter, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Adapter{cli: cli}, nil
}

func (a *Adapter) RunBuildContainer(ctx context.Context, params model.BuildRuntimeSpec) (string, io.ReadCloser, error) {
	// 1. Убеждаемся, что образ Kaniko есть на хосте
	kanikoImage := params.KanikoImage
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
		"--insecure",
		"--skip-tls-verify",
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
	go func() {
		defer pw.Close()
		defer rawLogs.Close()
		// StdCopy разделяет stdout и stderr, мы направляем оба потока в наш Pipe
		_, _ = stdcopy.StdCopy(pw, pw, rawLogs)
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
