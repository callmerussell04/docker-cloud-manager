package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/moby/go-archive"
)

type BuildContainerParams struct {
	WorkspaceDir   string
	ContextSubDir  string
	Dockerfile     string
	DestinationTag string
	MemoryBytes    int64
	CPUQuota       int64
	BuildArgs      map[string]string
}

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

func (a *Adapter) RunBuildContainer(ctx context.Context, params BuildContainerParams) (string, io.ReadCloser, error) {
	// 1. Убеждаемся, что образ Kaniko есть на хосте
	kanikoImage := "gcr.io/kaniko-project/executor:latest"
	_, err := a.cli.ImagePull(ctx, kanikoImage, image.PullOptions{})
	if err != nil {
		return "", nil, err
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
		"--cache=true",
		"--insecure",
		"--skip-tls-verify",
	}

	// Добавляем Build Args для Kaniko
	for k, v := range params.BuildArgs {
		cmd = append(cmd, fmt.Sprintf("--build-arg=%s=%s", k, v))
	}

	// 2. Настраиваем контейнер Kaniko
	resp, err := a.cli.ContainerCreate(ctx, &container.Config{
		Image: kanikoImage,
		Cmd:   cmd,
	}, &container.HostConfig{
		Resources: container.Resources{
			Memory:     params.MemoryBytes,
			MemorySwap: params.MemoryBytes * 2,
			CPUQuota:   params.CPUQuota,
			CPUPeriod:  100000,
		},
	}, &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			"dcm_net": {}, // Указываем ту же сеть, в которой находится Registry
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
