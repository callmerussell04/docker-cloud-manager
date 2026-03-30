package docker

import (
	"context"
	"io"
	"strconv"

	"github.com/docker/docker/api/types/container"
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
		"traefik.enable": "true",
		"traefik.http.routers." + params.ContainerName + ".rule":                      "Host(`" + params.Domain + "`)",
		"traefik.http.services." + params.ContainerName + ".loadbalancer.server.port": strconv.Itoa(params.InternalPort),
		"managed_by": "university-cloud",
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

	// TODO: look into this config ts might be a problem for dynamic quotas
	hostConfig := &container.HostConfig{
		NetworkMode: container.NetworkMode(params.NetworkName),
		Resources: container.Resources{
			Memory:            params.MemoryLimitBytes,
			MemorySwap:        params.MemoryLimitBytes * 2,
			MemoryReservation: params.MemoryReservation,
			CPUShares:         params.CPUShares,
		},
		Mounts: mounts,
	}

	netConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			params.NetworkName: {},
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

	// Читаем поток до конца, чтобы дождаться завершения скачивания
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
