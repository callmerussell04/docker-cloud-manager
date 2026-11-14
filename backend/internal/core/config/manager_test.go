package config_test

import "github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
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

	manager, err := config.NewManager(path, validSystemConfig())
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
	if cfg.BuildMaxDiskBytes != 10<<30 {
		t.Fatalf("BuildMaxDiskBytes = %d, want the 10 GiB default", cfg.BuildMaxDiskBytes)
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
	manager, err := config.NewManager(path, validSystemConfig())
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

func TestValidateSystemConfigRejectsInvalidBlockedDomainPrefixPattern(t *testing.T) {
	cfg := validSystemConfig()
	cfg.BlockedDomainPrefixPatterns = []string{"preview-["}

	if err := config.ValidateSystemConfig(cfg); err == nil {
		t.Fatal("ValidateSystemConfig() returned nil error for invalid blocked domain prefix pattern")
	}
}

// UpdateConfig wraps these errors as bad requests, whose messages reach the
// admin client verbatim, so they must not echo the rejected value.
func TestValidateSystemConfigErrorsNeverEchoTheirInput(t *testing.T) {
	const marker = "echomarker"
	for _, tc := range []struct {
		name   string
		mutate func(*config.SystemConfig)
	}{
		{name: "base domain", mutate: func(cfg *config.SystemConfig) { cfg.BaseDomain = marker + " domain" }},
		{name: "max log size", mutate: func(cfg *config.SystemConfig) { cfg.MaxLogSize = marker }},
		{name: "max log files", mutate: func(cfg *config.SystemConfig) { cfg.MaxLogFiles = marker }},
		{name: "container disk quota", mutate: func(cfg *config.SystemConfig) { cfg.ContainerDiskQuota = marker }},
		{name: "reserved domain prefix", mutate: func(cfg *config.SystemConfig) { cfg.ReservedDomainPrefixes = []string{"Bad_" + marker} }},
		{name: "blocked domain prefix pattern", mutate: func(cfg *config.SystemConfig) { cfg.BlockedDomainPrefixPatterns = []string{marker + "-["} }},
		{name: "git allowed host", mutate: func(cfg *config.SystemConfig) { cfg.GitAllowedHosts = []string{marker + ":22"} }},
		{name: "telemetry exec command", mutate: func(cfg *config.SystemConfig) { cfg.TelemetryAllowedExecCommands = []string{marker + "\n"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validSystemConfig()
			tc.mutate(&cfg)

			err := config.ValidateSystemConfig(cfg)
			if err == nil {
				t.Fatal("ValidateSystemConfig() returned nil error")
			}
			if strings.Contains(err.Error(), marker) {
				t.Fatalf("ValidateSystemConfig() error %q echoes the rejected value", err)
			}
		})
	}
}

func TestValidateSystemConfigBoundsVolumeUsageAccounting(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*config.SystemConfig)
		valid bool
	}{
		{name: "defaults", apply: func(*config.SystemConfig) {}, valid: true},
		{name: "zero interval", apply: func(c *config.SystemConfig) { c.VolumeUsageIntervalSeconds = 0 }},
		{name: "zero cycle budget", apply: func(c *config.SystemConfig) { c.VolumeUsageCycleBudgetSeconds = 0 }},
		{name: "zero entry budget", apply: func(c *config.SystemConfig) { c.VolumeUsageScanEntryBudget = 0 }},
		{name: "maximum entry budget", apply: func(c *config.SystemConfig) { c.VolumeUsageScanEntryBudget = config.MaxVolumeUsageScanEntryBudget }, valid: true},
		{name: "entry budget above maximum", apply: func(c *config.SystemConfig) { c.VolumeUsageScanEntryBudget = config.MaxVolumeUsageScanEntryBudget + 1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validSystemConfig()
			tt.apply(&cfg)
			err := config.ValidateSystemConfig(cfg)
			if tt.valid && err != nil {
				t.Fatalf("ValidateSystemConfig() error = %v", err)
			}
			if !tt.valid && err == nil {
				t.Fatal("ValidateSystemConfig() returned nil error")
			}
		})
	}
}

func TestValidateSystemConfigBoundsBuildMaxDisk(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
		valid bool
	}{
		{name: "default", bytes: 10 << 30, valid: true},
		{name: "minimum", bytes: 64 << 20, valid: true},
		{name: "below minimum", bytes: 64<<20 - 1},
		{name: "zero", bytes: 0},
		{name: "negative", bytes: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validSystemConfig()
			cfg.BuildMaxDiskBytes = tt.bytes
			err := config.ValidateSystemConfig(cfg)
			if tt.valid && err != nil {
				t.Fatalf("ValidateSystemConfig() error = %v", err)
			}
			if !tt.valid && err == nil {
				t.Fatal("ValidateSystemConfig() returned nil error")
			}
		})
	}
}

func TestValidateSystemConfigRequiresPositiveRegistryCleanupTiming(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*config.SystemConfig)
	}{
		{name: "zero interval", apply: func(c *config.SystemConfig) { c.RegistryCleanupIntervalSeconds = 0 }},
		{name: "negative interval", apply: func(c *config.SystemConfig) { c.RegistryCleanupIntervalSeconds = -1 }},
		{name: "zero job timeout", apply: func(c *config.SystemConfig) { c.RegistryCleanupJobTimeoutSeconds = 0 }},
		{name: "negative job timeout", apply: func(c *config.SystemConfig) { c.RegistryCleanupJobTimeoutSeconds = -1 }},
		{name: "zero gc max duration", apply: func(c *config.SystemConfig) { c.RegistryGCMaxDurationSeconds = 0 }},
		{name: "negative gc max duration", apply: func(c *config.SystemConfig) { c.RegistryGCMaxDurationSeconds = -1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validSystemConfig()
			tt.apply(&cfg)
			if err := config.ValidateSystemConfig(cfg); err == nil {
				t.Fatal("ValidateSystemConfig() returned nil error")
			}
		})
	}
}

func validSystemConfig() config.SystemConfig {
	return config.SystemConfig{
		BaseDomain:                           "localhost",
		DefaultMemoryReservation:             256 * 1024 * 1024,
		ReservedSystemMemory:                 0,
		OvercommitFactor:                     1.5,
		MaxBurstMultiplier:                   4,
		DefaultCPUReservation:                250,
		ReservedSystemCPU:                    0,
		CPUOvercommitFactor:                  4,
		MaxCPUBurstMultiplier:                4,
		ContainerCPUPeriod:                   100000,
		DefaultCPUShares:                     1024,
		ContainerStopTimeout:                 10,
		MaxLogSize:                           "10m",
		MaxLogFiles:                          "3",
		ContainerDiskQuota:                   "1G",
		ReservedDomainPrefixes:               []string{"admin", "api"},
		BlockedDomainPrefixPatterns:          []string{},
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
		BuildMaxDiskBytes:                    10 << 30,
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
		ComposeUploadMaxBytes:                100 << 20,
		ComposePipelineTimeoutMinutes:        30,
		ComposeDeployWorkerCount:             2,
		ComposeOutboxIntervalSeconds:         1,
		ComposeOutboxBatchSize:               10,
		ComposeDeployMaxAttempts:             3,
		ComposeBuildPollIntervalSeconds:      3,
		ComposeDependencyWaitTimeoutMinutes:  5,
		ComposeCoordinatorIntervalSeconds:    2,
		VolumeUsageIntervalSeconds:           60,
		VolumeUsageCycleBudgetSeconds:        15,
		VolumeUsageScanEntryBudget:           50000,
		RegistryCleanupIntervalSeconds:       5,
		RegistryCleanupJobTimeoutSeconds:     20,
		RegistryGCMaxDurationSeconds:         900,
		MemoryPressureMinFreeBytes:           512 * 1024 * 1024,
		MemoryPressureIntervalSeconds:        10,
		MemoryBoostMultiplier:                8,
		CPUBoostMultiplier:                   8,
		BoostMinFreeMemoryBytes:              2 * 1024 * 1024 * 1024,
		DefaultVolumeSizeBytes:               1 << 30,
		DefaultUserQuotaCPU:                  1,
		DefaultUserQuotaRAMMB:                2048,
		DefaultUserQuotaDiskMB:               5120,
		VolumeSizeEnforcement:                false,
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

func TestValidateSystemConfigBoundsMemoryPressureSettings(t *testing.T) {
	disabled := validSystemConfig()
	disabled.MemoryPressureMinFreeBytes = 0
	if err := config.ValidateSystemConfig(disabled); err != nil {
		t.Fatalf("a zero floor turns the worker off and is valid: %v", err)
	}
	boostOff := validSystemConfig()
	boostOff.MemoryBoostMultiplier = 1
	boostOff.CPUBoostMultiplier = 1
	boostOff.BoostMinFreeMemoryBytes = boostOff.MemoryPressureMinFreeBytes
	if err := config.ValidateSystemConfig(boostOff); err != nil {
		t.Fatalf("boost multipliers of 1 turn boost off and a high-water mark equal to the floor is valid: %v", err)
	}
	for _, tc := range []struct {
		name  string
		apply func(*config.SystemConfig)
	}{
		{name: "negative floor", apply: func(c *config.SystemConfig) { c.MemoryPressureMinFreeBytes = -1 }},
		{name: "zero interval", apply: func(c *config.SystemConfig) { c.MemoryPressureIntervalSeconds = 0 }},
		{name: "negative interval", apply: func(c *config.SystemConfig) { c.MemoryPressureIntervalSeconds = -1 }},
		{name: "zero memory boost multiplier", apply: func(c *config.SystemConfig) { c.MemoryBoostMultiplier = 0 }},
		{name: "memory boost multiplier above 64", apply: func(c *config.SystemConfig) { c.MemoryBoostMultiplier = 65 }},
		{name: "zero cpu boost multiplier", apply: func(c *config.SystemConfig) { c.CPUBoostMultiplier = 0 }},
		{name: "cpu boost multiplier above 64", apply: func(c *config.SystemConfig) { c.CPUBoostMultiplier = 65 }},
		{name: "boost free memory below the pressure floor", apply: func(c *config.SystemConfig) {
			c.BoostMinFreeMemoryBytes = c.MemoryPressureMinFreeBytes - 1
		}},
		{name: "zero default volume size", apply: func(c *config.SystemConfig) { c.DefaultVolumeSizeBytes = 0 }},
		{name: "default volume size below 1 MiB", apply: func(c *config.SystemConfig) { c.DefaultVolumeSizeBytes = 1<<20 - 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validSystemConfig()
			tc.apply(&cfg)
			if err := config.ValidateSystemConfig(cfg); err == nil {
				t.Fatal("ValidateSystemConfig() accepted the setting")
			}
		})
	}
}

func TestValidateSystemConfigRequiresPositiveDefaultUserQuotas(t *testing.T) {
	smallest := validSystemConfig()
	smallest.DefaultUserQuotaCPU = 0.001
	smallest.DefaultUserQuotaRAMMB, smallest.DefaultUserQuotaDiskMB = 1, 1
	if err := config.ValidateSystemConfig(smallest); err != nil {
		t.Fatalf("one millicore, 1 MB of memory and 1 MB of disk are valid defaults: %v", err)
	}
	// The rejected values carry a marker that must not reach the message,
	// which UpdateConfig returns to the admin client verbatim.
	const marker = "7777"
	for _, tc := range []struct {
		name  string
		apply func(*config.SystemConfig)
	}{
		{name: "zero cpu", apply: func(c *config.SystemConfig) { c.DefaultUserQuotaCPU = 0 }},
		{name: "negative cpu", apply: func(c *config.SystemConfig) { c.DefaultUserQuotaCPU = -7777 }},
		{name: "cpu below one millicore", apply: func(c *config.SystemConfig) { c.DefaultUserQuotaCPU = 0.00049 }},
		{name: "cpu not a number", apply: func(c *config.SystemConfig) { c.DefaultUserQuotaCPU = math.NaN() }},
		{name: "cpu infinite", apply: func(c *config.SystemConfig) { c.DefaultUserQuotaCPU = math.Inf(1) }},
		{name: "cpu negative infinite", apply: func(c *config.SystemConfig) { c.DefaultUserQuotaCPU = math.Inf(-1) }},
		{name: "cpu above the maximum", apply: func(c *config.SystemConfig) { c.DefaultUserQuotaCPU = 77770000 }},
		{name: "zero memory", apply: func(c *config.SystemConfig) { c.DefaultUserQuotaRAMMB = 0 }},
		{name: "negative memory", apply: func(c *config.SystemConfig) { c.DefaultUserQuotaRAMMB = -7777 }},
		{name: "zero disk", apply: func(c *config.SystemConfig) { c.DefaultUserQuotaDiskMB = 0 }},
		{name: "negative disk", apply: func(c *config.SystemConfig) { c.DefaultUserQuotaDiskMB = -7777 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validSystemConfig()
			tc.apply(&cfg)
			err := config.ValidateSystemConfig(cfg)
			if err == nil {
				t.Fatal("ValidateSystemConfig() accepted the setting")
			}
			if strings.Contains(err.Error(), marker) {
				t.Fatalf("ValidateSystemConfig() error %q echoes the rejected value", err)
			}
		})
	}
}

// Default user quotas are runtime settings: an update survives a restart, and
// a file written before they existed gets the first-run defaults.
func TestDefaultUserQuotasSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	manager, err := config.NewManager(path, validSystemConfig())
	if err != nil {
		t.Fatal(err)
	}
	updated := manager.Get()
	updated.DefaultUserQuotaCPU = 2.5
	updated.DefaultUserQuotaRAMMB = 4096
	updated.DefaultUserQuotaDiskMB = 10240
	if err := manager.Update(updated); err != nil {
		t.Fatal(err)
	}

	restarted, err := config.NewManager(path, validSystemConfig())
	if err != nil {
		t.Fatal(err)
	}
	got := restarted.Get()
	if got.DefaultUserQuotaCPU != 2.5 || got.DefaultUserQuotaRAMMB != 4096 || got.DefaultUserQuotaDiskMB != 10240 {
		t.Fatalf("default user quotas after restart = %v, %d, %d", got.DefaultUserQuotaCPU, got.DefaultUserQuotaRAMMB, got.DefaultUserQuotaDiskMB)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"default_user_quota_cpu", "default_user_quota_ram_mb", "default_user_quota_disk_mb"} {
		delete(raw, key)
	}
	older, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, older, 0o600); err != nil {
		t.Fatal(err)
	}
	backfilled, err := config.NewManager(path, validSystemConfig())
	if err != nil {
		t.Fatal(err)
	}
	got = backfilled.Get()
	if got.DefaultUserQuotaCPU != 1 || got.DefaultUserQuotaRAMMB != 2048 || got.DefaultUserQuotaDiskMB != 5120 {
		t.Fatalf("backfilled default user quotas = %v, %d, %d", got.DefaultUserQuotaCPU, got.DefaultUserQuotaRAMMB, got.DefaultUserQuotaDiskMB)
	}
}

func TestRegistryEndpointsRemainBootstrapSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	initial := validSystemConfig()
	initial.RegistryAPIURL = "https://old-registry:5000"
	if _, err := config.NewManager(path, initial); err != nil {
		t.Fatal(err)
	}
	next := initial
	next.RegistryAPIURL = "https://registry-backend:5000"
	next.RegistryPublicURL = "registry:5000"
	manager, err := config.NewManager(path, next)
	if err != nil {
		t.Fatal(err)
	}
	cfg := manager.Get()
	if cfg.RegistryAPIURL != next.RegistryAPIURL || cfg.RegistryPublicURL != next.RegistryPublicURL {
		t.Fatal("persisted config overrode Registry bootstrap endpoints")
	}
	cfg.RegistryAPIURL = "http://unauthenticated:5000"
	cfg.RegistryPublicURL = "unauthenticated:5000"
	if err := manager.Update(cfg); err != nil {
		t.Fatal(err)
	}
	cfg = manager.Get()
	if cfg.RegistryAPIURL != next.RegistryAPIURL || cfg.RegistryPublicURL != next.RegistryPublicURL {
		t.Fatal("runtime update changed Registry bootstrap endpoints")
	}
}

func TestNetworkNamesRemainBootstrapSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	initial := validSystemConfig()
	initial.ProxyNetworkName = "old_proxy"
	initial.BuildNetworkName = "old_build"
	if _, err := config.NewManager(path, initial); err != nil {
		t.Fatal(err)
	}
	next := initial
	next.ProxyNetworkName = "proxy_net"
	next.BuildNetworkName = "build_net"
	manager, err := config.NewManager(path, next)
	if err != nil {
		t.Fatal(err)
	}
	cfg := manager.Get()
	if cfg.ProxyNetworkName != "proxy_net" || cfg.BuildNetworkName != "build_net" {
		t.Fatal("persisted config overrode bootstrap network names")
	}
	cfg.ProxyNetworkName = "changed_proxy"
	cfg.BuildNetworkName = "changed_build"
	if err := manager.Update(cfg); err != nil {
		t.Fatal(err)
	}
	cfg = manager.Get()
	if cfg.ProxyNetworkName != "proxy_net" || cfg.BuildNetworkName != "build_net" {
		t.Fatal("runtime update changed bootstrap network names")
	}
}
