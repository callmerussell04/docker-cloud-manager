package docker

import (
	"context"
	"errors"
	"io"
	"strconv"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
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

func (a *Adapter) EnsureUserNetwork(ctx context.Context, networkName string) (string, error) {
	networks, err := a.cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", networkName)),
	})
	if err != nil {
		return "", err
	}

	if len(networks) > 0 {
		return networks[0].ID, nil
	}

	resp, err := a.cli.NetworkCreate(ctx, networkName, network.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{
			"managed_by": "docker-cloud-manager",
		},
	})
	if err != nil {
		return "", err
	}

	return resp.ID, nil
}

func (a *Adapter) CreateContainer(ctx context.Context, params CreateContainerParams) (string, error) {
	labels := map[string]string{
		"managed_by": "docker-cloud-manager",
	}

	if params.Domain != "" && params.InternalPort != 0 {
		labels["traefik.enable"] = "true"
		labels["traefik.http.routers."+params.ContainerName+".rule"] = "Host(`" + params.Domain + "`)"
		labels["traefik.http.services."+params.ContainerName+".loadbalancer.server.port"] = strconv.Itoa(params.InternalPort)
		labels["traefik.docker.network"] = "proxy_net"
	}

	var mounts []mount.Mount
	for _, m := range params.VolumeMounts {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeVolume,
			Source:   m.VolumeName,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		})
	}

	hostConfig := &container.HostConfig{
		NetworkMode: container.NetworkMode(params.NetworkName),
		Resources: container.Resources{
			Memory:            params.MemoryLimitBytes,
			MemorySwap:        params.MemoryLimitBytes * 2,
			MemoryReservation: params.MemoryReservation,
			CPUShares:         params.CPUShares,
		},
		Mounts: mounts,
		LogConfig: container.LogConfig{
			Type: "json-file",
			Config: map[string]string{
				"max-size": params.MaxLogSize,
				"max-file": params.MaxLogFiles,
			},
		},
	}

	if params.StorageQuota != "" {
		hostConfig.StorageOpt = map[string]string{
			"size": params.StorageQuota,
		}
	}

	// Настройка сети
	endpointSettings := &network.EndpointSettings{}

	// Если передан алиас (из Compose), добавляем его
	if params.NetworkAlias != "" {
		endpointSettings.Aliases = []string{params.NetworkAlias}
	}

	netConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			params.NetworkName: endpointSettings, // Подключаем к сети пользователя с алиасами
		},
	}

	containerConfig := &container.Config{
		Image:  params.ImageName,
		Env:    params.EnvVars,
		Labels: labels,
	}

	resp, err := a.cli.ContainerCreate(
		ctx,
		containerConfig,
		hostConfig,
		netConfig,
		nil,
		params.ContainerName,
	)
	if err != nil {
		return "", err
	}

	if params.Domain != "" && params.InternalPort != 0 {
		err = a.cli.NetworkConnect(ctx, "proxy_net", resp.ID, nil)
		if err != nil {
			// TODO: idk about this, probably remove this line
			//a.cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
			return "", err
		}
	}

	return resp.ID, nil
}

func (a *Adapter) StartContainer(ctx context.Context, dockerID string) error {
	return a.cli.ContainerStart(ctx, dockerID, container.StartOptions{})
}

func (a *Adapter) StopContainer(ctx context.Context, dockerID string, timeout int) error {
	stopOptions := container.StopOptions{
		Timeout: &timeout,
	}
	return a.cli.ContainerStop(ctx, dockerID, stopOptions)
}

func (a *Adapter) RemoveContainer(ctx context.Context, dockerID string, force bool) error {
	return a.cli.ContainerRemove(ctx, dockerID, container.RemoveOptions{
		Force:         force,
		RemoveVolumes: false,
	})
}

func (a *Adapter) CreateVolume(ctx context.Context, params CreateVolumeParams) (string, error) {
	driver := params.Driver
	if driver == "" {
		driver = "local"
	}

	vol, err := a.cli.VolumeCreate(ctx, volume.CreateOptions{
		Name:       params.VolumeName,
		Driver:     driver,
		DriverOpts: params.DriverOpts,
		Labels: map[string]string{
			"managed_by": "docker-cloud-manager",
		},
	})
	if err != nil {
		return "", err
	}

	return vol.Name, nil
}

func (a *Adapter) RemoveVolume(ctx context.Context, volumeName string, force bool) error {
	return a.cli.VolumeRemove(ctx, volumeName, force)
}

func (a *Adapter) Ping(ctx context.Context) error {
	_, err := a.cli.Ping(ctx)
	return err
}

func (a *Adapter) Close() error {
	if a.cli != nil {
		return a.cli.Close()
	}
	return nil
}

func (a *Adapter) PullImage(ctx context.Context, imageName string) error {
	out, err := a.cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(io.Discard, out)
	return err
}

func (a *Adapter) InspectContainer(ctx context.Context, dockerID string) (*container.InspectResponse, error) {
	containerJSON, err := a.cli.ContainerInspect(ctx, dockerID)
	if err != nil {
		return nil, err
	}
	return &containerJSON, nil
}

func (a *Adapter) InspectVolume(ctx context.Context, volumeName string) (*volume.Volume, error) {
	vol, err := a.cli.VolumeInspect(ctx, volumeName)
	if err != nil {
		return nil, err
	}
	return &vol, nil
}

func (a *Adapter) RemoveImage(ctx context.Context, imageID string, force bool) error {
	_, err := a.cli.ImageRemove(ctx, imageID, image.RemoveOptions{
		Force:         force,
		PruneChildren: true,
	})
	return err
}

func (a *Adapter) UpdateContainerResources(ctx context.Context, dockerID string, memoryLimit, memoryReservation, cpuShares int64) error {
	updateConfig := container.UpdateConfig{
		Resources: container.Resources{
			Memory:            memoryLimit,
			MemorySwap:        memoryLimit * 2,
			MemoryReservation: memoryReservation,
			CPUShares:         cpuShares,
		},
	}

	_, err := a.cli.ContainerUpdate(ctx, dockerID, updateConfig)
	return err
}

func (a *Adapter) ImageExists(ctx context.Context, imageTag string) (bool, error) {
	_, err := a.cli.ImageInspect(ctx, imageTag)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (a *Adapter) ListenEvents(ctx context.Context) (<-chan events.Message, <-chan error) {
	options := events.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("type", "container"),
		),
	}
	return a.cli.Events(ctx, options)
}

func (a *Adapter) PruneSystem(ctx context.Context) error {
	_, err := a.cli.ImagesPrune(ctx, filters.Args{})
	if err != nil {
		return err
	}

	opts := build.CachePruneOptions{All: false}
	_, err = a.cli.BuildCachePrune(ctx, opts)
	return err
}

func (a *Adapter) RunRegistryGarbageCollect(ctx context.Context, registryContainerName string) error {
	execConfig := container.ExecOptions{
		Cmd: []string{
			"/bin/registry",
			"garbage-collect",
			"/etc/docker/registry/config.yml",
			"--delete-untagged=true", // Удалять слои, на которые больше нет ссылок
		},
		AttachStdout: false,
		AttachStderr: false,
	}

	execID, err := a.cli.ContainerExecCreate(ctx, registryContainerName, execConfig)
	if err != nil {
		return err
	}

	err = a.cli.ContainerExecStart(ctx, execID.ID, container.ExecStartOptions{})
	if err != nil {
		return err
	}

	// Ждем завершения выполнения команды
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			inspect, err := a.cli.ContainerExecInspect(ctx, execID.ID)
			if err != nil {
				return err
			}
			if !inspect.Running {
				if inspect.ExitCode != 0 {
					return errors.New("registry gc exited with non-zero code")
				}
				return nil
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
}
