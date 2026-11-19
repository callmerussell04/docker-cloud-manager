package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
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
	"github.com/google/uuid"
)

// tenantOOMScoreAdj is the OOM score adjustment of every tenant container.
// When the host runs out of memory, the kernel's OOM killer then picks a
// tenant workload before the platform's own services, which keep Docker's
// default of zero.
const tenantOOMScoreAdj = 500

type Adapter struct {
	cli            *client.Client
	hostPathPrefix string
	pullAuth       func(context.Context, string) (string, error)
	cgroupParent   string
}

type AdapterOption func(*Adapter)

func WithPullAuthorization(auth func(context.Context, string) (string, error)) AdapterOption {
	return func(a *Adapter) { a.pullAuth = auth }
}

// WithCgroupParent places every container the adapter creates under parent,
// so the host can cap what user containers take together. Empty keeps
// Docker's default parent.
func WithCgroupParent(parent string) AdapterOption {
	return func(a *Adapter) { a.cgroupParent = strings.TrimSpace(parent) }
}

func WithHostPathPrefix(path string) AdapterOption {
	return func(a *Adapter) {
		path = strings.TrimSpace(path)
		if path == "" || path == string(os.PathSeparator) {
			a.hostPathPrefix = ""
			return
		}
		a.hostPathPrefix = filepath.Clean(path)
	}
}

func NewAdapter(opts ...AdapterOption) (*Adapter, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	adapter := &Adapter{cli: cli}
	for _, opt := range opts {
		opt(adapter)
	}
	return adapter, nil
}

func (a *Adapter) EnsureUserNetwork(ctx context.Context, networkName string) (string, error) {
	networkID, err := a.findUserNetwork(ctx, networkName)
	if err != nil {
		return "", err
	}
	if networkID != "" {
		return networkID, nil
	}

	resp, err := a.cli.NetworkCreate(ctx, networkName, network.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{
			"managed_by": "docker-cloud-manager",
		},
	})
	if err != nil {
		networkID, findErr := a.findUserNetwork(ctx, networkName)
		if findErr == nil && networkID != "" {
			return networkID, nil
		}
		return "", err
	}

	return resp.ID, nil
}

func (a *Adapter) findUserNetwork(ctx context.Context, networkName string) (string, error) {
	networks, err := a.cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", networkName)),
	})
	if err != nil {
		return "", err
	}
	for _, n := range networks {
		if n.Name == networkName {
			return n.ID, nil
		}
	}
	return "", nil
}

// errImageDeclaresTraefikLabels names no label: the keys are image content.
var errImageDeclaresTraefikLabels = apperrors.New(apperrors.ErrBadRequest, "image declares traefik labels, which are reserved for the platform")

// routingSafeImageID returns the content-addressed ID of imageName after
// checking that the image declares no Traefik labels. Docker copies image
// labels onto every container created from the image and a container can only
// override a key, never unset it, so an image's own traefik.* labels would
// reach Traefik's Docker provider as routers the platform never validated.
// The caller creates the container from the returned ID, so the image that
// was checked is the one that runs even if its tag is repointed meanwhile.
func (a *Adapter) routingSafeImageID(ctx context.Context, imageName string) (string, error) {
	inspect, err := a.cli.ImageInspect(ctx, imageName)
	if err != nil {
		return "", err
	}
	if inspect.Config != nil {
		for key := range inspect.Config.Labels {
			if isTraefikLabel(key) {
				return "", errImageDeclaresTraefikLabels
			}
		}
	}
	if inspect.ID == "" {
		return "", errors.New("image inspect returned no image id")
	}
	return inspect.ID, nil
}

// isTraefikLabel matches the way Traefik selects labels: by the traefik root
// segment, ignoring case.
func isTraefikLabel(key string) bool {
	root, _, _ := strings.Cut(key, ".")
	return strings.EqualFold(root, "traefik")
}

// CreateContainer creates a container whose Traefik labels are exactly the
// ones set here: an image that declares its own is rejected as a bad request.
func (a *Adapter) CreateContainer(ctx context.Context, params model.ContainerRuntimeSpec) (string, error) {
	imageID, err := a.routingSafeImageID(ctx, params.ImageName)
	if err != nil {
		return "", err
	}

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
		OomScoreAdj: tenantOOMScoreAdj,
		Resources: container.Resources{
			Memory:            params.MemoryLimitBytes,
			CgroupParent:      a.cgroupParent,
			MemorySwap:        memorySwap,
			MemoryReservation: params.MemoryReservation,
			CPUShares:         params.CPUShares,
			CPUQuota:          params.CPUQuota,
			CPUPeriod:         params.CPUPeriod,
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
		Image:      imageID,
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
			_ = a.cli.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true}) // best-effort removal of the unconnected container; the connect error is returned
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

// localVolumeDriver is Docker's built-in volume driver; its "size" option
// sets a project quota on the volume directory when the host supports it.
const localVolumeDriver = "local"

// CreateVolume creates a volume. A positive SizeBytes is sent to the local
// driver as its "size" option, so the kernel refuses writes past it; Docker
// refuses the create when the host cannot enforce it.
func (a *Adapter) CreateVolume(ctx context.Context, params model.VolumeRuntimeSpec) (string, error) {
	opts := volume.CreateOptions{
		Name: params.VolumeName,
		Labels: map[string]string{
			"managed_by":        "docker-cloud-manager",
			"dcm.resource_type": "volume",
			"dcm.volume_id":     params.VolumeID,
			"dcm.volume_name":   params.Name,
			"dcm.owner_id":      params.OwnerID,
			"dcm.project_id":    params.ProjectID,
		},
	}
	if params.SizeBytes > 0 {
		opts.Driver = localVolumeDriver
		opts.DriverOpts = map[string]string{"size": strconv.FormatInt(params.SizeBytes, 10)}
	}
	vol, err := a.cli.VolumeCreate(ctx, opts)
	if err != nil {
		return "", err
	}

	return vol.Name, nil
}

// volumeSizeCheckBytes is the size of the throwaway volume the host check
// creates; it is never written to.
const volumeSizeCheckBytes = 16 << 20

// volumeSizeCheckRemoveTimeout bounds removing the check volume, which runs
// even when the caller's context has ended.
const volumeSizeCheckRemoveTimeout = 10 * time.Second

// CheckVolumeSizeSupport reports whether Docker accepts a size for a local
// volume on this host. It creates a small sized volume under a unique name
// and removes it on every path. Docker refusing the size (an invalid
// parameter, such as "quota size requested but no quota support") reports
// false with no error; any other failure, including a failed removal, is an
// error.
func (a *Adapter) CheckVolumeSizeSupport(ctx context.Context) (supported bool, err error) {
	name := "dcm-volume-size-check-" + uuid.NewString()
	defer func() {
		removeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), volumeSizeCheckRemoveTimeout)
		defer cancel()
		if removeErr := a.cli.VolumeRemove(removeCtx, name, true); removeErr != nil && !cerrdefs.IsNotFound(removeErr) {
			supported = false
			err = errors.Join(err, fmt.Errorf("remove volume size check volume: %w", removeErr))
		}
	}()
	_, err = a.cli.VolumeCreate(ctx, volume.CreateOptions{
		Name:       name,
		Driver:     localVolumeDriver,
		DriverOpts: map[string]string{"size": strconv.Itoa(volumeSizeCheckBytes)},
		Labels: map[string]string{
			"managed_by":        "docker-cloud-manager",
			"dcm.resource_type": "volume_size_check",
		},
	})
	if err != nil {
		if cerrdefs.IsInvalidArgument(err) {
			return false, nil
		}
		return false, fmt.Errorf("create volume size check volume: %w", err)
	}
	return true, nil
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

// containerdSnapshotterDriverType is the driver-type Docker reports when its
// image store is a containerd snapshotter, which ignores the size option.
const containerdSnapshotterDriverType = "io.containerd.snapshotter.v1"

// ReadDockerHost reads docker info once: the daemon's data directory, and
// whether Docker enforces the root filesystem size option (StorageOpt "size")
// on this host, judged from its storage driver. A failed read is
// RootfsQuotaUnknown with no root directory, and the error.
func (a *Adapter) ReadDockerHost(ctx context.Context) (model.DockerHost, error) {
	info, err := a.cli.Info(ctx)
	if err != nil {
		return model.DockerHost{RootfsQuota: model.RootfsQuotaUnknown}, fmt.Errorf("read docker info: %w", err)
	}
	return model.DockerHost{RootfsQuota: rootfsQuotaStatus(info.Driver, info.DriverStatus), RootDir: info.DockerRootDir}, nil
}

// rootfsQuotaStatus classifies a storage driver. Overlay2 on XFS counts as
// enforced because Docker refuses the size option there when the mount lacks
// project quotas, so a created container has its cap.
func rootfsQuotaStatus(driver string, driverStatus [][2]string) model.RootfsQuotaStatus {
	status := make(map[string]string, len(driverStatus))
	for _, pair := range driverStatus {
		status[strings.ToLower(pair[0])] = pair[1]
	}
	if status["driver-type"] == containerdSnapshotterDriverType {
		return model.RootfsQuotaNotEnforced
	}
	switch driver {
	case "overlay2":
		backing, reported := status["backing filesystem"]
		switch {
		case !reported:
			return model.RootfsQuotaUnknown
		case strings.EqualFold(backing, "xfs"):
			return model.RootfsQuotaEnforced
		default:
			return model.RootfsQuotaNotEnforced
		}
	case "btrfs", "zfs", "devicemapper":
		return model.RootfsQuotaEnforced
	default:
		return model.RootfsQuotaUnknown
	}
}

func (a *Adapter) PullImage(ctx context.Context, imageName string) error {
	options := image.PullOptions{}
	if a.pullAuth != nil {
		auth, err := a.pullAuth(ctx, imageName)
		if err != nil {
			return err
		}
		options.RegistryAuth = auth
	}
	out, err := a.cli.ImagePull(ctx, imageName, options)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }() // read-only stream; close failure is not actionable

	decoder := json.NewDecoder(out)
	for {
		var progress struct {
			Error string `json:"error"`
		}
		if err := decoder.Decode(&progress); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if progress.Error != "" {
			return apperrors.New(apperrors.ErrUnavailable, "image pull failed")
		}
	}
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
		CPUQuota:          containerJSON.HostConfig.CPUQuota,
		CPUPeriod:         containerJSON.HostConfig.CPUPeriod,
		Restart:           string(containerJSON.HostConfig.RestartPolicy.Name),
		State: model.ContainerState{
			Running:   containerJSON.State.Running,
			Status:    containerJSON.State.Status,
			ExitCode:  containerJSON.State.ExitCode,
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

func (a *Adapter) volumeMountpointPath(mountpoint string) string {
	if a.hostPathPrefix == "" || mountpoint == "" || !filepath.IsAbs(mountpoint) {
		return mountpoint
	}
	cleanMountpoint := filepath.Clean(mountpoint)
	if cleanMountpoint == string(os.PathSeparator) {
		return a.hostPathPrefix
	}
	return filepath.Join(a.hostPathPrefix, strings.TrimPrefix(cleanMountpoint, string(os.PathSeparator)))
}

func (a *Adapter) RemoveImage(ctx context.Context, imageID string, force bool) error {
	_, err := a.cli.ImageRemove(ctx, imageID, image.RemoveOptions{
		Force:         force,
		PruneChildren: true,
	})
	return err
}

func (a *Adapter) UpdateContainerResources(ctx context.Context, dockerID string, resources model.ContainerResourceUpdate) error {
	updateConfig := container.UpdateConfig{
		Resources: container.Resources{
			Memory:            resources.MemoryLimitBytes,
			MemorySwap:        int64(float64(resources.MemoryLimitBytes) * resources.MemorySwapMultiplier),
			MemoryReservation: resources.MemoryReservation,
			CPUShares:         resources.CPUShares,
			CPUQuota:          resources.CPUQuota,
			CPUPeriod:         resources.CPUPeriod,
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

// StartRegistryGarbageCollect starts Registry garbage collection as a detached
// exec in the Registry container and returns its exec ID. It does not wait:
// the caller polls InspectRegistryGarbageCollect, so it can hold its lease and
// stop waiting on cancellation.
func (a *Adapter) StartRegistryGarbageCollect(ctx context.Context, registryContainerName string) (string, error) {
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
		return "", fmt.Errorf("create registry gc exec: %w", err)
	}
	if err := a.cli.ContainerExecStart(ctx, execID.ID, container.ExecStartOptions{Detach: true}); err != nil {
		return "", fmt.Errorf("start registry gc exec: %w", err)
	}
	return execID.ID, nil
}

// InspectRegistryGarbageCollect reports whether the garbage collection exec
// still runs and, once it has ended, its exit code. An exec the daemon no
// longer knows, as after a Registry restart, is apperrors.ErrNotFound.
func (a *Adapter) InspectRegistryGarbageCollect(ctx context.Context, execID string) (bool, int, error) {
	inspect, err := a.cli.ContainerExecInspect(ctx, execID)
	if cerrdefs.IsNotFound(err) {
		return false, 0, apperrors.Wrap(apperrors.ErrNotFound, apperrors.ErrNotFound.Error(), fmt.Errorf("inspect registry gc exec: %w", err))
	}
	if err != nil {
		return false, 0, fmt.Errorf("inspect registry gc exec: %w", err)
	}
	return inspect.Running, inspect.ExitCode, nil
}

func (a *Adapter) RemoveNetwork(ctx context.Context, networkName string) error {
	err := a.cli.NetworkRemove(ctx, networkName)
	if cerrdefs.IsNotFound(err) {
		return nil
	}
	return err
}

// ListRunningBuildContainers lists the running Kaniko containers Builder
// started, with the build and owner their labels name. A container whose
// identity labels do not parse is left out: it cannot be attributed.
func (a *Adapter) ListRunningBuildContainers(ctx context.Context) ([]model.BuildContainer, error) {
	containers, err := a.cli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", "managed_by=docker-cloud-manager"),
			filters.Arg("label", "dcm.resource_type=build"),
			filters.Arg("status", "running"),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("list build containers: %w", err)
	}
	builds := make([]model.BuildContainer, 0, len(containers))
	for _, c := range containers {
		buildID, buildErr := uuid.Parse(c.Labels["dcm.build_id"])
		ownerID, ownerErr := uuid.Parse(c.Labels["dcm.owner_id"])
		if buildErr != nil || ownerErr != nil {
			continue
		}
		builds = append(builds, model.BuildContainer{DockerID: c.ID, BuildID: buildID, OwnerID: ownerID})
	}
	return builds, nil
}

// GetContainerUsage reads one stats sample of a running container without
// waiting for a second, unlike GetContainerStats. Its working set is the
// memory the kernel cannot simply drop: usage less inactive file cache, the
// figure docker stats shows.
func (a *Adapter) GetContainerUsage(ctx context.Context, dockerID string) (model.ContainerUsage, error) {
	statsResp, err := a.cli.ContainerStatsOneShot(ctx, dockerID)
	if err != nil {
		return model.ContainerUsage{}, err
	}
	defer func() { _ = statsResp.Body.Close() }() // read-only stream; close failure is not actionable

	var v struct {
		MemoryStats struct {
			Usage int64            `json:"usage"`
			Limit int64            `json:"limit"`
			Stats map[string]int64 `json:"stats"`
		} `json:"memory_stats"`
		CPUStats struct {
			CPUUsage struct {
				TotalUsage int64 `json:"total_usage"`
			} `json:"cpu_usage"`
		} `json:"cpu_stats"`
	}
	if err := json.NewDecoder(statsResp.Body).Decode(&v); err != nil {
		return model.ContainerUsage{}, err
	}
	usage := model.ContainerUsage{
		WorkingSetBytes:     v.MemoryStats.Usage,
		MemoryLimitBytes:    v.MemoryStats.Limit,
		CPUUsageNanoseconds: v.CPUStats.CPUUsage.TotalUsage,
	}
	// cgroup v2 reports inactive_file; cgroup v1 reports total_inactive_file.
	inactiveFile, ok := v.MemoryStats.Stats["inactive_file"]
	if !ok {
		inactiveFile = v.MemoryStats.Stats["total_inactive_file"]
	}
	if inactiveFile > 0 && inactiveFile < v.MemoryStats.Usage {
		usage.WorkingSetBytes -= inactiveFile
	}
	return usage, nil
}

func (a *Adapter) GetContainerStats(ctx context.Context, dockerID string) (model.ContainerStats, error) {
	statsResp, err := a.cli.ContainerStats(ctx, dockerID, false)
	if err != nil {
		return model.ContainerStats{}, err
	}
	defer func() { _ = statsResp.Body.Close() }() // read-only stream; close failure is not actionable

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
