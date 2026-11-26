package coretest

import (
	"context"
	"io"
	"log/slog"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/google/uuid"
)

func UserContext(ownerID uuid.UUID) context.Context {
	return accessscope.WithUserScope(context.Background(), ownerID, "test-user", "user")
}

func AdminContext(adminID uuid.UUID) context.Context {
	return accessscope.WithAdminScope(context.Background(), adminID, "test-admin", "admin")
}

func DiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func SystemConfig() config.SystemConfig {
	return config.SystemConfig{
		DefaultMemoryReservation:            256 * 1024 * 1024,
		ReservedSystemMemory:                0,
		OvercommitFactor:                    1.5,
		MaxBurstMultiplier:                  4,
		DefaultCPUReservation:               250,
		ReservedSystemCPU:                   0,
		CPUOvercommitFactor:                 4,
		MaxCPUBurstMultiplier:               4,
		ContainerStopTimeout:                10,
		MaxLogSize:                          "10m",
		ContainerDiskQuota:                  "1G",
		MaxVolumesPerUser:                   5,
		MaxContainersPerUser:                10,
		MaxQueuedBuildsPerUser:              10,
		MaxQueuedComposeDeploysPerUser:      5,
		ContainerTTLHours:                   24,
		ContainerPidsLimit:                  256,
		ImageBuildsEnabled:                  true,
		BuildMemoryBytes:                    512 * 1024 * 1024,
		BuildCPUMillicores:                  1000,
		MaxBuildTimeMinutes:                 10,
		MaxConcurrentBuilds:                 2,
		MaxArchiveSizeBytes:                 50 << 20,
		MaxUnpackedSizeBytes:                500 * 1024 * 1024,
		MaxBuildLogSizeBytes:                5 * 1024 * 1024,
		BuildMaxDiskBytes:                   10 << 30,
		ComposeUploadMaxBytes:               100 << 20,
		ComposePipelineTimeoutMinutes:       30,
		ComposeDependencyWaitTimeoutMinutes: 5,
		MemoryPressureMinFreeBytes:          512 * 1024 * 1024,
		MemoryBoostMultiplier:               8,
		CPUBoostMultiplier:                  8,
		BoostMinFreeMemoryBytes:             2 * 1024 * 1024 * 1024,
		DefaultVolumeSizeBytes:              256 << 20,
		DefaultUserQuotaCPU:                 1,
		DefaultUserQuotaRAMMB:               2048,
		DefaultUserQuotaDiskMB:              5120,
		VolumeSizeEnforcement:               false,
		GitSourcesEnabled:                   true,
		GitAllowedHosts:                     []string{"github.com", "gitlab.com", "bitbucket.org"},
		GitCloneTimeoutSeconds:              60,
		GitMaxRepositoryBytes:               200 * 1024 * 1024,
		TelemetryTerminalIdleTimeoutSeconds: 300,
		TelemetryTerminalMaxDurationSeconds: 3600,
		TelemetryAllowedExecCommands:        []string{"/bin/sh", "/bin/bash", "/busybox/sh"},
		Fixed: config.Fixed{
			BaseDomain:                           "localhost",
			ContainerCPUPeriod:                   100000,
			DefaultCPUShares:                     1024,
			MaxLogFiles:                          "3",
			RegistryAPIURL:                       "registry:5000",
			RegistryPublicURL:                    "localhost:5000",
			ContainerMemorySwapMultiplier:        2,
			ProxyNetworkName:                     "proxy_net",
			RegistryContainerName:                "registry",
			BuildCPUPeriod:                       100000,
			BuildMemorySwapMultiplier:            2,
			BuildPidsLimit:                       512,
			BuildNetworkName:                     "build_net",
			KanikoImage:                          "gcr.io/kaniko-project/executor:latest",
			BuildCancelPollIntervalSeconds:       2,
			TTLWorkerIntervalSeconds:             60,
			GCWorkerIntervalMinutes:              60,
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
			ReportsUsageSnapshotIntervalSeconds:  config.DefaultReportsUsageSnapshotIntervalSeconds,
			ComposeDeployWorkerCount:             2,
			ComposeOutboxIntervalSeconds:         1,
			ComposeOutboxBatchSize:               10,
			ComposeDeployMaxAttempts:             3,
			ComposeCoordinatorIntervalSeconds:    2,
			VolumeUsageIntervalSeconds:           60,
			VolumeUsageCycleBudgetSeconds:        15,
			VolumeUsageScanEntryBudget:           50000,
			RegistryCleanupIntervalSeconds:       5,
			RegistryCleanupJobTimeoutSeconds:     20,
			RegistryGCMaxDurationSeconds:         900,
			MemoryPressureIntervalSeconds:        10,
			TelemetryMaxLogTailLines:             1000,
			TelemetryMaxLogStreamsPerUser:        5,
			TelemetryMaxTerminalSessionsPerUser:  2,
			TelemetryMaxCommandArgs:              8,
			TelemetryMaxCommandArgBytes:          128,
			TelemetryWSReadLimitBytes:            4096,
		},
	}
}
