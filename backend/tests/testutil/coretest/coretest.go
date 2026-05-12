package coretest

import (
	"context"
	"io"
	"log/slog"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
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
		BaseDomain:                           "localhost",
		DefaultMemoryReservation:             256 * 1024 * 1024,
		ReservedSystemMemory:                 0,
		OvercommitFactor:                     1.5,
		MaxBurstMultiplier:                   4,
		DefaultCPUShares:                     1024,
		HighLoadCPUShares:                    512,
		HighLoadContainerCount:               5,
		ContainerStopTimeout:                 10,
		MaxLogSize:                           "10m",
		MaxLogFiles:                          "3",
		ContainerDiskQuota:                   "1G",
		MaxVolumesPerUser:                    5,
		MaxContainersPerUser:                 10,
		RegistryAPIURL:                       "registry:5000",
		RegistryPublicURL:                    "localhost:5000",
		ContainerTTLHours:                    24,
		ContainerPidsLimit:                   256,
		ContainerMemorySwapMultiplier:        2,
		ProxyNetworkName:                     "proxy_net",
		RegistryContainerName:                "registry",
		ImageBuildsEnabled:                   true,
		BuildMemoryBytes:                     512 * 1024 * 1024,
		BuildCPUQuota:                        100000,
		BuildCPUPeriod:                       100000,
		BuildMemorySwapMultiplier:            2,
		BuildPidsLimit:                       512,
		BuildNetworkName:                     "build_net",
		KanikoImage:                          "gcr.io/kaniko-project/executor:latest",
		MaxBuildTimeMinutes:                  10,
		MaxConcurrentBuilds:                  2,
		MaxUploadSizeBytes:                   50 << 20,
		MaxArchiveSizeBytes:                  50 << 20,
		MaxUnpackedSizeBytes:                 500 * 1024 * 1024,
		MaxBuildLogSizeBytes:                 5 * 1024 * 1024,
		BuildCancelPollIntervalSeconds:       2,
		TTLWorkerIntervalSeconds:             60,
		GCWorkerIntervalMinutes:              60,
		StaleBuildTimeoutMinutes:             30,
		EventSyncIntervalSeconds:             30,
		EventReconnectDelaySeconds:           5,
		BuildOutboxIntervalSeconds:           1,
		BuildOutboxBatchSize:                 10,
		ReportsUsageSnapshotIntervalSeconds:  config.DefaultReportsUsageSnapshotIntervalSeconds,
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
		GitMaxRepositoryBytes:                200 * 1024 * 1024,
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
}
