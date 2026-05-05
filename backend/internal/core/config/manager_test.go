package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestManagerBackfillsLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{
		"base_domain": "example.test",
		"default_memory_reservation_bytes": 268435456,
		"reserved_system_memory_bytes": 0,
		"overcommit_factor": 1.5,
		"max_burst_multiplier": 4,
		"default_cpu_shares": 1024,
		"high_load_cpu_shares": 512,
		"high_load_container_count": 5,
		"container_stop_timeout": 10,
		"max_log_size": "10m",
		"max_log_files": "3",
		"container_disk_quota": "1G",
		"max_volumes_per_user": 5,
		"max_containers_per_user": 10,
		"registry_api_url": "registry:5000",
		"registry_public_url": "localhost:5000",
		"container_ttl": 86400000000000
	}`), 0644); err != nil {
		t.Fatal(err)
	}

	manager, err := NewManager(path, validSystemConfig())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	cfg := manager.Get()
	if cfg.ContainerTTLHours != 24 {
		t.Fatalf("ContainerTTLHours = %d, want 24", cfg.ContainerTTLHours)
	}
	if cfg.ContainerPidsLimit != 256 || cfg.BuildPidsLimit != 512 {
		t.Fatalf("runtime limits were not backfilled: %+v", cfg)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["container_ttl"]; ok {
		t.Fatal("legacy container_ttl key was not removed")
	}
	if _, ok := raw["container_ttl_hours"]; !ok {
		t.Fatal("container_ttl_hours key was not written")
	}
}

func TestManagerRejectsInvalidUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	manager, err := NewManager(path, validSystemConfig())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	cfg := validSystemConfig()
	cfg.BuildPidsLimit = 0
	if err := manager.Update(cfg); err == nil {
		t.Fatal("Update() returned nil error for invalid config")
	}

	if got := manager.Get().BuildPidsLimit; got != validSystemConfig().BuildPidsLimit {
		t.Fatalf("BuildPidsLimit after rejected update = %d", got)
	}
}

func validSystemConfig() SystemConfig {
	return SystemConfig{
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
		ComposeUploadMaxBytes:                100 << 20,
		ComposePipelineTimeoutMinutes:        30,
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
