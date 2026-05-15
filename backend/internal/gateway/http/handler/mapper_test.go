package handler

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
)

func TestCreateContainerInputFromDTO(t *testing.T) {
	req := dto.CreateContainerDTO{
		Name:         "web",
		ImageTag:     "nginx:latest",
		InternalPort: 80,
		EnvVars:      map[string]string{"APP_ENV": "test"},
		DomainPrefix: "web",
		VolumeMounts: []dto.VolumeMountDTO{
			{VolumeID: "volume-id", MountPath: "/data", IsReadOnly: true},
		},
	}

	got := createContainerInputFromDTO(req)

	if got.Name != req.Name || got.ImageTag != req.ImageTag || got.InternalPort != req.InternalPort || got.DomainPrefix != req.DomainPrefix {
		t.Fatalf("input fields were not mapped correctly: %+v", got)
	}
	if got.EnvVars["APP_ENV"] != "test" {
		t.Fatalf("EnvVars were not mapped")
	}
	if len(got.VolumeMounts) != 1 || got.VolumeMounts[0].VolumeID != "volume-id" || !got.VolumeMounts[0].IsReadOnly {
		t.Fatalf("VolumeMounts were not mapped: %+v", got.VolumeMounts)
	}
}

func TestSystemConfigDTORoundTrip(t *testing.T) {
	req := dto.SystemConfigDTO{
		BaseDomain:                           "localhost",
		DefaultMemoryReservationBytes:        1,
		ReservedSystemMemoryBytes:            2,
		OvercommitFactor:                     1.5,
		MaxBurstMultiplier:                   4,
		DefaultCpuShares:                     1024,
		HighLoadCpuShares:                    512,
		HighLoadContainerCount:               5,
		ContainerStopTimeout:                 10,
		MaxLogSize:                           "10m",
		MaxLogFiles:                          "3",
		ContainerDiskQuota:                   "1G",
		MaxVolumesPerUser:                    5,
		MaxContainersPerUser:                 10,
		RegistryApiUrl:                       "registry:5000",
		RegistryPublicUrl:                    "localhost:5000",
		ContainerTtlHours:                    24,
		ContainerPidsLimit:                   256,
		ContainerMemorySwapMultiplier:        2,
		ProxyNetworkName:                     "proxy_net",
		RegistryContainerName:                "registry",
		ImageBuildsEnabled:                   true,
		BuildMemoryBytes:                     512,
		BuildCpuQuota:                        100000,
		BuildCpuPeriod:                       100000,
		BuildMemorySwapMultiplier:            2,
		BuildPidsLimit:                       512,
		BuildNetworkName:                     "build_net",
		KanikoImage:                          "kaniko:test",
		MaxBuildTimeMinutes:                  10,
		MaxConcurrentBuilds:                  2,
		MaxUploadSizeBytes:                   50 << 20,
		MaxArchiveSizeBytes:                  50 << 20,
		MaxUnpackedSizeBytes:                 500 << 20,
		MaxBuildLogSizeBytes:                 5 << 20,
		BuildCancelPollIntervalSeconds:       2,
		TtlWorkerIntervalSeconds:             60,
		GcWorkerIntervalMinutes:              60,
		StaleBuildTimeoutMinutes:             30,
		EventSyncIntervalSeconds:             30,
		EventReconnectDelaySeconds:           5,
		BuildOutboxIntervalSeconds:           1,
		BuildOutboxBatchSize:                 10,
		ContainerCreateWorkerCount:           2,
		ContainerCreateMaxAttempts:           3,
		ContainerCreateTimeoutMinutes:        30,
		MaxQueuedContainerCreatesPerUser:     5,
		ContainerCreateOutboxIntervalSeconds: 1,
		ContainerCreateOutboxBatchSize:       10,
		ComposeUploadMaxBytes:                100 << 20,
		ComposePipelineTimeoutMinutes:        30,
		ComposeDeployWorkerCount:             2,
		ComposeOutboxIntervalSeconds:         1,
		ComposeOutboxBatchSize:               10,
		ComposeDeployMaxAttempts:             3,
		MaxQueuedComposeDeploysPerUser:       5,
		ComposeBuildPollIntervalSeconds:      3,
		ComposeDependencyWaitTimeoutMinutes:  5,
		ComposeDependencyPollIntervalSeconds: 2,
		ComposeCoordinatorIntervalSeconds:    2,
		MaxStagedSourceBytesPerUser:          1024 << 20,
		MaxQueuedBuildsPerUser:               10,
		HostMinFreeDiskBytes:                 1024 << 20,
		GitSourcesEnabled:                    true,
		GitAllowedHosts:                      []string{"github.com", "gitlab.com", "bitbucket.org"},
		GitCloneTimeoutSeconds:               60,
		GitMaxRepositoryBytes:                200 << 20,
		TelemetryMaxLogTailLines:             1000,
		TelemetryMaxLogStreamsPerUser:        5,
		TelemetryMaxTerminalSessionsPerUser:  2,
		TelemetryTerminalIdleTimeoutSeconds:  300,
		TelemetryTerminalMaxDurationSeconds:  3600,
		TelemetryAllowedExecCommands:         []string{"/bin/sh", "/bin/bash", "/busybox/sh"},
		TelemetryMaxCommandArgs:              8,
		TelemetryMaxCommandArgBytes:          128,
		TelemetryWSReadLimitBytes:            4096,
	}

	got := systemConfigToDTO(systemConfigFromDTO(req))
	if !reflect.DeepEqual(got, req) {
		t.Fatalf("system config round trip mismatch:\n got: %+v\nwant: %+v", got, req)
	}
}

func TestContainersToDTO(t *testing.T) {
	items := []model.Container{
		{
			ID:            "container-id",
			DockerID:      "docker-id",
			Name:          "web",
			ImageTag:      "nginx:latest",
			InternalPort:  80,
			DomainPrefix:  "web",
			Status:        "running",
			CreatedAt:     123,
			OwnerID:       "owner-id",
			OwnerUsername: "alice",
		},
	}

	got := containersToDTO(items)

	if len(got) != 1 {
		t.Fatalf("containersToDTO() len = %d, want 1", len(got))
	}
	if got[0].ID != items[0].ID || got[0].OwnerUsername != items[0].OwnerUsername {
		t.Fatalf("container was not mapped correctly: %+v", got[0])
	}
}

func TestSystemMonitoringToDTO(t *testing.T) {
	stats := model.SystemMonitoring{
		CPUPercent:             42.5,
		MemoryTotalBytes:       100,
		MemoryUsedBytes:        70,
		MemoryAvailableBytes:   30,
		DiskTotalBytes:         200,
		DiskUsedBytes:          150,
		DiskFreeBytes:          50,
		DCMReservedMemoryBytes: 64,
		DCMDiskUsedBytes:       32,
		ContainersTotal:        5,
		ContainersRunning:      3,
		ContainersStopped:      1,
		ContainersError:        1,
		ContainersMissing:      0,
		VolumesTotal:           2,
		ImagesTotal:            4,
		BuildsTotal:            6,
		ProjectsTotal:          7,
		ObservedAt:             123,
	}

	got := systemMonitoringToDTO(stats)
	if got.CPUPercent != stats.CPUPercent || got.DCMReservedMemoryBytes != stats.DCMReservedMemoryBytes || got.ProjectsTotal != stats.ProjectsTotal {
		t.Fatalf("system monitoring was not mapped correctly: %+v", got)
	}
}

func TestUserResourceDTOsOmitOperationalFields(t *testing.T) {
	containers, err := json.Marshal(containersToUserDTO([]model.Container{{
		ID:            "container-id",
		DockerID:      "docker-id",
		OwnerID:       "owner-id",
		OwnerUsername: "alice",
	}}))
	if err != nil {
		t.Fatalf("marshal containers: %v", err)
	}
	builds, err := json.Marshal(buildsToUserDTO([]model.Build{{
		ID:            "build-id",
		LogFilePath:   "build-logs/file.log",
		OwnerID:       "owner-id",
		OwnerUsername: "alice",
	}}))
	if err != nil {
		t.Fatalf("marshal builds: %v", err)
	}

	payload := string(containers) + string(builds)
	for _, forbidden := range []string{"docker_id", "owner_id", "owner_username", "log_file_path"} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("user DTO payload contains operational field %q: %s", forbidden, payload)
		}
	}
}

func TestAdminResourceDTOsIncludeOperationalFields(t *testing.T) {
	containers, err := json.Marshal(containersToDTO([]model.Container{{
		ID:            "container-id",
		DockerID:      "docker-id",
		OwnerID:       "owner-id",
		OwnerUsername: "alice",
	}}))
	if err != nil {
		t.Fatalf("marshal containers: %v", err)
	}
	builds, err := json.Marshal(buildsToDTO([]model.Build{{
		ID:            "build-id",
		LogFilePath:   "build-logs/file.log",
		OwnerID:       "owner-id",
		OwnerUsername: "alice",
	}}))
	if err != nil {
		t.Fatalf("marshal builds: %v", err)
	}

	payload := string(containers) + string(builds)
	for _, required := range []string{"docker_id", "owner_id", "owner_username", "log_file_path"} {
		if !strings.Contains(payload, required) {
			t.Fatalf("admin DTO payload does not contain operational field %q: %s", required, payload)
		}
	}
}
