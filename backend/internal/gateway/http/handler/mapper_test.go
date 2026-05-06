package handler

import (
	"reflect"
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
		ComposeUploadMaxBytes:                100 << 20,
		ComposePipelineTimeoutMinutes:        30,
		ComposeDeployWorkerCount:             2,
		ComposeOutboxIntervalSeconds:         1,
		ComposeOutboxBatchSize:               10,
		ComposeDeployMaxAttempts:             3,
		ComposeBuildPollIntervalSeconds:      3,
		ComposeDependencyWaitTimeoutMinutes:  5,
		ComposeDependencyPollIntervalSeconds: 2,
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
