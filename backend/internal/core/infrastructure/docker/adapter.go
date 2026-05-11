package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
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

func (a *Adapter) CreateContainer(ctx context.Context, params model.ContainerRuntimeSpec) (string, error) {
	labels := map[string]string{
		"managed_by":        "docker-cloud-manager",
		"dcm.resource_type": "container",
		"dcm.container_id":  params.ContainerID,
		"dcm.owner_id":      params.OwnerID,
		"dcm.generation":    strconv.Itoa(params.Generation),
	}
	if params.ProjectID != "" {
		labels["dcm.project_id"] = params.ProjectID
	}

	if params.Domain != "" && params.InternalPort != 0 {
		labels["traefik.enable"] = "true"
		labels["traefik.http.routers."+params.ContainerName+".rule"] = "Host(`" + params.Domain + "`)"
		labels["traefik.http.services."+params.ContainerName+".loadbalancer.server.port"] = strconv.Itoa(params.InternalPort)
		labels["traefik.docker.network"] = params.ProxyNetworkName
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

	pidsLimit := params.PidsLimit
	memorySwap := int64(float64(params.MemoryLimitBytes) * params.MemorySwapMultiplier)
	hostConfig := &container.HostConfig{
		NetworkMode: container.NetworkMode(params.NetworkName),
		SecurityOpt: []string{"no-new-privileges:true"},
		CapDrop:     []string{"NET_RAW"},
		Resources: container.Resources{
			Memory:            params.MemoryLimitBytes,
			MemorySwap:        memorySwap,
			MemoryReservation: params.MemoryReservation,
			CPUShares:         params.CPUShares,
			PidsLimit:         &pidsLimit,
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

	if params.Restart != "" {
		hostConfig.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyMode(params.Restart)}
	}

	containerConfig := &container.Config{
		Image:      params.ImageName,
		Env:        params.EnvVars,
		Labels:     labels,
		Cmd:        params.Command,
		Entrypoint: params.Entrypoint,
	}

	if params.Healthcheck != nil {
		containerConfig.Healthcheck = &container.HealthConfig{
			Test:        params.Healthcheck.Test,
			Interval:    params.Healthcheck.Interval,
			Timeout:     params.Healthcheck.Timeout,
			StartPeriod: params.Healthcheck.StartPeriod,
			Retries:     params.Healthcheck.Retries,
		}
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
		err = a.cli.NetworkConnect(ctx, params.ProxyNetworkName, resp.ID, nil)
		if err != nil {
			_ = a.cli.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
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

func (a *Adapter) CreateVolume(ctx context.Context, params model.VolumeRuntimeSpec) (string, error) {
	vol, err := a.cli.VolumeCreate(ctx, volume.CreateOptions{
		Name: params.VolumeName,
		Labels: map[string]string{
			"managed_by":        "docker-cloud-manager",
			"dcm.resource_type": "volume",
			"dcm.volume_id":     params.VolumeID,
			"dcm.owner_id":      params.OwnerID,
			"dcm.project_id":    params.ProjectID,
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

func (a *Adapter) InspectContainer(ctx context.Context, dockerID string) (model.ContainerInspection, error) {
	containerJSON, err := a.cli.ContainerInspect(ctx, dockerID)
	if err != nil {
		return model.ContainerInspection{}, err
	}

	inspection := model.ContainerInspection{
		Name:              containerJSON.Name,
		Mounts:            make([]model.ContainerMountSpec, 0, len(containerJSON.Mounts)),
		MemoryLimitBytes:  containerJSON.HostConfig.Memory,
		MemoryReservation: containerJSON.HostConfig.MemoryReservation,
		CPUShares:         containerJSON.HostConfig.CPUShares,
		Restart:           string(containerJSON.HostConfig.RestartPolicy.Name),
		State: model.ContainerState{
			Running:  containerJSON.State.Running,
			Status:   containerJSON.State.Status,
			ExitCode: containerJSON.State.ExitCode,
			OOMKilled: containerJSON.State.OOMKilled,
		},
	}
	if containerJSON.Config != nil {
		inspection.Image = containerJSON.Config.Image
		inspection.Env = containerJSON.Config.Env
		inspection.Command = containerJSON.Config.Cmd
		inspection.Entrypoint = containerJSON.Config.Entrypoint
		if containerJSON.Config.Healthcheck != nil {
			inspection.Healthcheck = &model.Healthcheck{
				Test:        containerJSON.Config.Healthcheck.Test,
				Interval:    containerJSON.Config.Healthcheck.Interval,
				Timeout:     containerJSON.Config.Healthcheck.Timeout,
				StartPeriod: containerJSON.Config.Healthcheck.StartPeriod,
				Retries:     containerJSON.Config.Healthcheck.Retries,
			}
		}
	}
	if containerJSON.State.Health != nil {
		healthStatus := containerJSON.State.Health.Status
		inspection.State.HealthStatus = &healthStatus
	}
	for _, m := range containerJSON.Mounts {
		if m.Type != mount.TypeVolume {
			continue
		}
		inspection.Mounts = append(inspection.Mounts, model.ContainerMountSpec{
			VolumeName: m.Name,
			Target:     m.Destination,
			ReadOnly:   !m.RW,
		})
	}
	return inspection, nil
}

func (a *Adapter) InspectVolume(ctx context.Context, volumeName string) (model.VolumeInspection, error) {
	vol, err := a.cli.VolumeInspect(ctx, volumeName)
	if err != nil {
		return model.VolumeInspection{}, err
	}
	return model.VolumeInspection{
		Name:       vol.Name,
		Labels:     vol.Labels,
		Mountpoint: vol.Mountpoint,
	}, nil
}

func (a *Adapter) GetVolumeUsageBytes(ctx context.Context, volumeName string) (int64, error) {
	vol, err := a.cli.VolumeInspect(ctx, volumeName)
	if err != nil {
		return 0, err
	}
	var total int64
	err = filepath.WalkDir(vol.Mountpoint, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}

func (a *Adapter) RemoveImage(ctx context.Context, imageID string, force bool) error {
	_, err := a.cli.ImageRemove(ctx, imageID, image.RemoveOptions{
		Force:         force,
		PruneChildren: true,
	})
	return err
}

func (a *Adapter) UpdateContainerResources(ctx context.Context, dockerID string, memoryLimit, memoryReservation, cpuShares int64, memorySwapMultiplier float64) error {
	updateConfig := container.UpdateConfig{
		Resources: container.Resources{
			Memory:            memoryLimit,
			MemorySwap:        int64(float64(memoryLimit) * memorySwapMultiplier),
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

func (a *Adapter) ListenEvents(ctx context.Context) (<-chan model.ContainerEvent, <-chan error) {
	options := events.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("type", "container"),
		),
	}
	dockerEvents, errCh := a.cli.Events(ctx, options)
	eventCh := make(chan model.ContainerEvent)

	go func() {
		defer close(eventCh)
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-dockerEvents:
				if !ok {
					return
				}
				event := model.ContainerEvent{
					Type:        string(msg.Type),
					Action:      string(msg.Action),
					DockerID:    msg.Actor.ID,
					ContainerID: msg.Actor.Attributes["dcm.container_id"],
				}
				if generation, err := strconv.Atoi(msg.Actor.Attributes["dcm.generation"]); err == nil {
					event.Generation = generation
				}
				if exitCode, err := strconv.Atoi(msg.Actor.Attributes["exitCode"]); err == nil {
					event.ExitCode = &exitCode
				}
				if oomKilled, err := strconv.ParseBool(msg.Actor.Attributes["oomKilled"]); err == nil {
					event.OOMKilled = oomKilled
				}
				select {
				case eventCh <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return eventCh, errCh
}

func (a *Adapter) PruneSystem(ctx context.Context) error {
	pruneArgs := filters.NewArgs(filters.Arg("until", "24h"))

	_, err := a.cli.ImagesPrune(ctx, pruneArgs)
	if err != nil {
		return err
	}

	opts := build.CachePruneOptions{All: false, Filters: pruneArgs}
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

func (a *Adapter) RemoveNetwork(ctx context.Context, networkName string) error {
	return a.cli.NetworkRemove(ctx, networkName)
}

func (a *Adapter) GetContainerStats(ctx context.Context, dockerID string) (model.ContainerStats, error) {
	statsResp, err := a.cli.ContainerStats(ctx, dockerID, false)
	if err != nil {
		return model.ContainerStats{}, err
	}
	defer statsResp.Body.Close()

	var v struct {
		MemoryStats struct {
			Usage int64 `json:"usage"`
			Limit int64 `json:"limit"`
		} `json:"memory_stats"`
		CPUStats struct {
			CPUUsage struct {
				TotalUsage  uint64   `json:"total_usage"`
				PercpuUsage []uint64 `json:"percpu_usage"`
			} `json:"cpu_usage"`
			SystemUsage uint64 `json:"system_cpu_usage"`
			OnlineCPUs  uint32 `json:"online_cpus"`
		} `json:"cpu_stats"`
		PreCPUStats struct {
			CPUUsage struct {
				TotalUsage uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			SystemUsage uint64 `json:"system_cpu_usage"`
		} `json:"precpu_stats"`
		Networks map[string]struct {
			RxBytes int64 `json:"rx_bytes"`
			TxBytes int64 `json:"tx_bytes"`
		} `json:"networks"`
	}

	if err := json.NewDecoder(statsResp.Body).Decode(&v); err != nil {
		return model.ContainerStats{}, err
	}

	var cpuPercent float64
	cpuDelta := float64(v.CPUStats.CPUUsage.TotalUsage - v.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(v.CPUStats.SystemUsage - v.PreCPUStats.SystemUsage)

	if systemDelta > 0.0 && cpuDelta > 0.0 {
		if len(v.CPUStats.CPUUsage.PercpuUsage) > 0 {
			cpuPercent = (cpuDelta / systemDelta) * float64(len(v.CPUStats.CPUUsage.PercpuUsage)) * 100.0
		} else if v.CPUStats.OnlineCPUs > 0 {
			cpuPercent = (cpuDelta / systemDelta) * float64(v.CPUStats.OnlineCPUs) * 100.0
		}
	}

	var netRx, netTx int64
	for _, net := range v.Networks {
		netRx += net.RxBytes
		netTx += net.TxBytes
	}

	return model.ContainerStats{
		CPUPercentage:    cpuPercent,
		MemoryUsageBytes: v.MemoryStats.Usage,
		MemoryLimitBytes: v.MemoryStats.Limit,
		NetworkRxBytes:   netRx,
		NetworkTxBytes:   netTx,
	}, nil
}
