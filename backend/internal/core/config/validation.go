package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
)

func ValidateSystemConfig(cfg SystemConfig) error {
	if cfg.BaseDomain == "" || strings.ContainsAny(cfg.BaseDomain, " `/\\") {
		return fmt.Errorf("invalid base domain")
	}
	if cfg.DefaultMemoryReservation <= 0 || cfg.ReservedSystemMemory < 0 {
		return fmt.Errorf("memory limits must be positive")
	}
	if cfg.OvercommitFactor <= 0 || cfg.OvercommitFactor > 10 {
		return fmt.Errorf("overcommit factor must be between 0 and 10")
	}
	if cfg.MaxBurstMultiplier <= 0 || cfg.MaxBurstMultiplier > 10 {
		return fmt.Errorf("max burst multiplier must be between 1 and 10")
	}
	if cfg.DefaultCPUReservation <= 0 || cfg.ReservedSystemCPU < 0 {
		return fmt.Errorf("cpu reservation limits must be positive")
	}
	if cfg.CPUOvercommitFactor <= 0 || cfg.CPUOvercommitFactor > 10 {
		return fmt.Errorf("cpu overcommit factor must be between 0 and 10")
	}
	if cfg.MaxCPUBurstMultiplier <= 0 || cfg.MaxCPUBurstMultiplier > 10 {
		return fmt.Errorf("max cpu burst multiplier must be between 1 and 10")
	}
	if cfg.ContainerCPUPeriod <= 0 {
		return fmt.Errorf("container cpu period must be positive")
	}
	if cfg.DefaultCPUShares <= 0 || cfg.HighLoadCPUShares <= 0 {
		return fmt.Errorf("cpu shares must be positive")
	}
	if cfg.HighLoadContainerCount <= 0 || cfg.ContainerStopTimeout <= 0 {
		return fmt.Errorf("container counters and timeouts must be positive")
	}
	if err := validation.DockerSize(cfg.MaxLogSize); err != nil {
		return fmt.Errorf("invalid max log size: %w", err)
	}
	if err := validation.PositiveNumberString(cfg.MaxLogFiles); err != nil {
		return fmt.Errorf("invalid max log files: %w", err)
	}
	if err := validation.DockerSize(cfg.ContainerDiskQuota); err != nil {
		return fmt.Errorf("invalid container disk quota: %w", err)
	}
	seenPrefixes := make(map[string]struct{}, len(cfg.ReservedDomainPrefixes))
	for _, prefix := range cfg.ReservedDomainPrefixes {
		if prefix == "" {
			return fmt.Errorf("reserved domain prefixes must not contain empty values")
		}
		if err := validation.DomainPrefix(prefix); err != nil {
			return fmt.Errorf("invalid reserved domain prefix %q: %w", prefix, err)
		}
		normalized := strings.ToLower(prefix)
		if _, ok := seenPrefixes[normalized]; ok {
			return fmt.Errorf("reserved domain prefixes must be unique")
		}
		seenPrefixes[normalized] = struct{}{}
	}
	seenBlockedPatterns := make(map[string]struct{}, len(cfg.BlockedDomainPrefixPatterns))
	for _, pattern := range cfg.BlockedDomainPrefixPatterns {
		trimmed := strings.TrimSpace(pattern)
		if trimmed == "" {
			return fmt.Errorf("blocked domain prefix patterns must not contain empty values")
		}
		if _, err := regexp.Compile("^(?:" + trimmed + ")$"); err != nil {
			return fmt.Errorf("invalid blocked domain prefix pattern %q: %w", pattern, err)
		}
		if _, ok := seenBlockedPatterns[trimmed]; ok {
			return fmt.Errorf("blocked domain prefix patterns must be unique")
		}
		seenBlockedPatterns[trimmed] = struct{}{}
	}
	if cfg.MaxVolumesPerUser <= 0 || cfg.MaxContainersPerUser <= 0 {
		return fmt.Errorf("user resource limits must be positive")
	}
	if cfg.RegistryAPIURL == "" || cfg.RegistryPublicURL == "" {
		return fmt.Errorf("registry urls are required")
	}
	if cfg.ContainerTTLHours < 0 {
		return fmt.Errorf("container ttl must not be negative")
	}
	if cfg.ContainerPidsLimit <= 0 {
		return fmt.Errorf("container pids limit must be positive")
	}
	if cfg.ContainerMemorySwapMultiplier < 1 || cfg.ContainerMemorySwapMultiplier > 10 {
		return fmt.Errorf("container memory swap multiplier must be between 1 and 10")
	}
	if cfg.ProxyNetworkName == "" {
		return fmt.Errorf("proxy network name is required")
	}
	if cfg.RegistryContainerName == "" {
		return fmt.Errorf("registry container name is required")
	}
	if cfg.BuildMemoryBytes <= 0 || cfg.BuildCPUQuota <= 0 || cfg.BuildCPUPeriod <= 0 || cfg.BuildPidsLimit <= 0 {
		return fmt.Errorf("build resource limits must be positive")
	}
	if cfg.BuildMemorySwapMultiplier < 1 || cfg.BuildMemorySwapMultiplier > 10 {
		return fmt.Errorf("build memory swap multiplier must be between 1 and 10")
	}
	if cfg.BuildNetworkName == "" || cfg.KanikoImage == "" {
		return fmt.Errorf("build network name and kaniko image are required")
	}
	if cfg.MaxBuildTimeMinutes <= 0 || cfg.MaxConcurrentBuilds <= 0 {
		return fmt.Errorf("build time and concurrency must be positive")
	}
	if cfg.MaxUploadSizeBytes <= 0 || cfg.MaxArchiveSizeBytes <= 0 || cfg.MaxUnpackedSizeBytes <= 0 || cfg.MaxBuildLogSizeBytes <= 0 {
		return fmt.Errorf("build size limits must be positive")
	}
	if cfg.BuildCancelPollIntervalSeconds <= 0 {
		return fmt.Errorf("build cancel poll interval must be positive")
	}
	if cfg.TTLWorkerIntervalSeconds <= 0 || cfg.GCWorkerIntervalMinutes <= 0 || cfg.StaleBuildTimeoutMinutes <= 0 {
		return fmt.Errorf("worker intervals and stale build timeout must be positive")
	}
	if cfg.EventSyncIntervalSeconds <= 0 || cfg.EventReconnectDelaySeconds <= 0 {
		return fmt.Errorf("event worker intervals must be positive")
	}
	if cfg.BuildOutboxIntervalSeconds <= 0 || cfg.BuildOutboxBatchSize <= 0 {
		return fmt.Errorf("build outbox settings must be positive")
	}
	if cfg.ContainerCreateWorkerCount <= 0 || cfg.ContainerCreateMaxAttempts <= 0 || cfg.ContainerCreateTimeoutMinutes <= 0 {
		return fmt.Errorf("container create worker settings must be positive")
	}
	if cfg.MaxQueuedContainerCreatesPerUser < 0 || cfg.ContainerCreateOutboxIntervalSeconds <= 0 || cfg.ContainerCreateOutboxBatchSize <= 0 {
		return fmt.Errorf("container create queue settings are invalid")
	}
	if cfg.ReportsUsageSnapshotIntervalSeconds < MinReportsUsageSnapshotIntervalSeconds || cfg.ReportsUsageSnapshotIntervalSeconds > MaxReportsUsageSnapshotIntervalSeconds {
		return fmt.Errorf("reports usage snapshot interval must be between %d and %d seconds", MinReportsUsageSnapshotIntervalSeconds, MaxReportsUsageSnapshotIntervalSeconds)
	}
	if cfg.MaxStagedSourceBytesPerUser < 0 || cfg.MaxQueuedBuildsPerUser < 0 || cfg.MaxQueuedComposeDeploysPerUser < 0 || cfg.HostMinFreeDiskBytes < 0 {
		return fmt.Errorf("resource safety limits must not be negative")
	}
	if cfg.ComposeUploadMaxBytes <= 0 || cfg.ComposePipelineTimeoutMinutes <= 0 {
		return fmt.Errorf("compose upload and timeout settings must be positive")
	}
	if cfg.ComposeDeployWorkerCount <= 0 || cfg.ComposeOutboxIntervalSeconds <= 0 || cfg.ComposeOutboxBatchSize <= 0 || cfg.ComposeDeployMaxAttempts <= 0 {
		return fmt.Errorf("compose queue and worker settings must be positive")
	}
	if cfg.ComposeBuildPollIntervalSeconds <= 0 || cfg.ComposeDependencyWaitTimeoutMinutes <= 0 || cfg.ComposeDependencyPollIntervalSeconds <= 0 {
		return fmt.Errorf("compose polling settings must be positive")
	}
	if cfg.ComposeCoordinatorIntervalSeconds <= 0 {
		return fmt.Errorf("compose coordinator interval must be positive")
	}
	if cfg.GitCloneTimeoutSeconds <= 0 || cfg.GitMaxRepositoryBytes <= 0 {
		return fmt.Errorf("git source limits must be positive")
	}
	if cfg.GitSourcesEnabled && len(cfg.GitAllowedHosts) == 0 {
		return fmt.Errorf("git allowed hosts are required")
	}
	seenGitHosts := make(map[string]struct{}, len(cfg.GitAllowedHosts))
	for _, host := range cfg.GitAllowedHosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" || strings.ContainsAny(host, " :/\\\x00\r\n") || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
			return fmt.Errorf("invalid git allowed host")
		}
		if _, ok := seenGitHosts[host]; ok {
			return fmt.Errorf("git allowed hosts must be unique")
		}
		seenGitHosts[host] = struct{}{}
	}
	if cfg.TelemetryMaxLogTailLines <= 0 || cfg.TelemetryMaxLogStreamsPerUser <= 0 || cfg.TelemetryMaxTerminalSessionsPerUser <= 0 {
		return fmt.Errorf("telemetry stream limits must be positive")
	}
	if cfg.TelemetryTerminalIdleTimeoutSeconds <= 0 || cfg.TelemetryTerminalMaxDurationSeconds <= 0 {
		return fmt.Errorf("telemetry terminal timeouts must be positive")
	}
	if len(cfg.TelemetryAllowedExecCommands) == 0 {
		return fmt.Errorf("telemetry allowed exec commands are required")
	}
	seenCommands := make(map[string]struct{}, len(cfg.TelemetryAllowedExecCommands))
	for _, command := range cfg.TelemetryAllowedExecCommands {
		if command == "" || strings.ContainsAny(command, "\x00\r\n") {
			return fmt.Errorf("telemetry allowed exec commands must not contain empty or control values")
		}
		if _, ok := seenCommands[command]; ok {
			return fmt.Errorf("telemetry allowed exec commands must be unique")
		}
		seenCommands[command] = struct{}{}
	}
	if cfg.TelemetryMaxCommandArgs < 0 || cfg.TelemetryMaxCommandArgBytes <= 0 || cfg.TelemetryWSReadLimitBytes <= 0 {
		return fmt.Errorf("telemetry command and websocket limits must be positive")
	}
	return nil
}
