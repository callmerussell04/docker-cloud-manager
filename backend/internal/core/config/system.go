package config

import "math"

const (
	DefaultReportsUsageSnapshotIntervalSeconds = int64(300)
	MinReportsUsageSnapshotIntervalSeconds     = int64(60)
	MaxReportsUsageSnapshotIntervalSeconds     = int64(86400)
	MaxVolumeUsageScanEntryBudget              = 10_000_000
	// MaxBoostMultiplier bounds MemoryBoostMultiplier and CPUBoostMultiplier.
	MaxBoostMultiplier = int64(64)
	// MinVolumeSizeBytes is the smallest volume size, for the default and for
	// a size given at creation.
	MinVolumeSizeBytes = int64(1 << 20)
	// MaxUserQuotaCPU bounds a CPU quota in cores, so its millicores stay an
	// exact int64.
	MaxUserQuotaCPU = 1_000_000.0
	// DefaultBuildMaxDiskBytes and MinBuildMaxDiskBytes are the default and
	// the smallest disk one build may write.
	DefaultBuildMaxDiskBytes = int64(10 << 30)
	MinBuildMaxDiskBytes     = int64(64 << 20)
)

// CPUMillicores converts a CPU quota in cores to the whole millicores that
// admission counts, rounding to the nearest millicore.
func CPUMillicores(cores float64) int64 {
	return int64(math.Round(cores * 1000))
}

type SystemConfig struct {
	BaseDomain                  string   `json:"base_domain"`
	DefaultMemoryReservation    int64    `json:"default_memory_reservation_bytes"`
	ReservedSystemMemory        int64    `json:"reserved_system_memory_bytes"`
	OvercommitFactor            float64  `json:"overcommit_factor"`
	MaxBurstMultiplier          int64    `json:"max_burst_multiplier"`
	DefaultCPUReservation       int64    `json:"default_cpu_reservation_millicores"`
	ReservedSystemCPU           int64    `json:"reserved_system_cpu_millicores"`
	CPUOvercommitFactor         float64  `json:"cpu_overcommit_factor"`
	MaxCPUBurstMultiplier       int64    `json:"max_cpu_burst_multiplier"`
	ContainerCPUPeriod          int64    `json:"container_cpu_period"`
	DefaultCPUShares            int64    `json:"default_cpu_shares"`
	ContainerStopTimeout        int      `json:"container_stop_timeout"`
	MaxLogSize                  string   `json:"max_log_size"`
	MaxLogFiles                 string   `json:"max_log_files"`
	ContainerDiskQuota          string   `json:"container_disk_quota"`
	ReservedDomainPrefixes      []string `json:"reserved_domain_prefixes"`
	BlockedDomainPrefixPatterns []string `json:"blocked_domain_prefix_patterns"`
	MaxVolumesPerUser           int      `json:"max_volumes_per_user"`
	MaxContainersPerUser        int      `json:"max_containers_per_user"`
	RegistryAPIURL              string   `json:"registry_api_url"`
	RegistryPublicURL           string   `json:"registry_public_url"`
	ContainerTTLHours           int64    `json:"container_ttl_hours"`

	ContainerPidsLimit            int64   `json:"container_pids_limit"`
	ContainerMemorySwapMultiplier float64 `json:"container_memory_swap_multiplier"`
	ProxyNetworkName              string  `json:"proxy_network_name"`
	RegistryContainerName         string  `json:"registry_container_name"`

	ImageBuildsEnabled             bool    `json:"image_builds_enabled"`
	BuildMemoryBytes               int64   `json:"build_memory_bytes"`
	BuildCPUQuota                  int64   `json:"build_cpu_quota"`
	BuildCPUPeriod                 int64   `json:"build_cpu_period"`
	BuildMemorySwapMultiplier      float64 `json:"build_memory_swap_multiplier"`
	BuildPidsLimit                 int64   `json:"build_pids_limit"`
	BuildNetworkName               string  `json:"build_network_name"`
	KanikoImage                    string  `json:"kaniko_image"`
	MaxBuildTimeMinutes            int64   `json:"max_build_time_minutes"`
	MaxConcurrentBuilds            int     `json:"max_concurrent_builds"`
	MaxUploadSizeBytes             int64   `json:"max_upload_size_bytes"`
	MaxArchiveSizeBytes            int64   `json:"max_archive_size_bytes"`
	MaxUnpackedSizeBytes           int64   `json:"max_unpacked_size_bytes"`
	MaxBuildLogSizeBytes           int64   `json:"max_build_log_size_bytes"`
	BuildCancelPollIntervalSeconds int64   `json:"build_cancel_poll_interval_seconds"`
	// BuildMaxDiskBytes is the disk one build may write. Each build slot
	// holds it against the host disk floor.
	BuildMaxDiskBytes int64 `json:"build_max_disk_bytes"`

	TTLWorkerIntervalSeconds             int64 `json:"ttl_worker_interval_seconds"`
	GCWorkerIntervalMinutes              int64 `json:"gc_worker_interval_minutes"`
	StaleBuildTimeoutMinutes             int64 `json:"stale_build_timeout_minutes"`
	EventSyncIntervalSeconds             int64 `json:"event_sync_interval_seconds"`
	EventReconnectDelaySeconds           int64 `json:"event_reconnect_delay_seconds"`
	BuildOutboxIntervalSeconds           int64 `json:"build_outbox_interval_seconds"`
	BuildOutboxBatchSize                 int   `json:"build_outbox_batch_size"`
	ContainerCreateWorkerCount           int   `json:"container_create_worker_count"`
	ContainerCreateMaxAttempts           int   `json:"container_create_max_attempts"`
	ContainerCreateTimeoutMinutes        int64 `json:"container_create_timeout_minutes"`
	MaxQueuedContainerCreatesPerUser     int   `json:"max_queued_container_creates_per_user"`
	ContainerCreateOutboxIntervalSeconds int64 `json:"container_create_outbox_interval_seconds"`
	ContainerCreateOutboxBatchSize       int   `json:"container_create_outbox_batch_size"`
	ReportsUsageSnapshotIntervalSeconds  int64 `json:"reports_usage_snapshot_interval_seconds"`
	MaxStagedSourceBytesPerUser          int64 `json:"max_staged_source_bytes_per_user"`
	MaxQueuedBuildsPerUser               int   `json:"max_queued_builds_per_user"`
	ComposeUploadMaxBytes                int64 `json:"compose_upload_max_bytes"`
	ComposePipelineTimeoutMinutes        int64 `json:"compose_pipeline_timeout_minutes"`
	ComposeDeployWorkerCount             int   `json:"compose_deploy_worker_count"`
	ComposeOutboxIntervalSeconds         int64 `json:"compose_outbox_interval_seconds"`
	ComposeOutboxBatchSize               int   `json:"compose_outbox_batch_size"`
	ComposeDeployMaxAttempts             int   `json:"compose_deploy_max_attempts"`
	MaxQueuedComposeDeploysPerUser       int   `json:"max_queued_compose_deploys_per_user"`
	ComposeBuildPollIntervalSeconds      int64 `json:"compose_build_poll_interval_seconds"`
	ComposeDependencyWaitTimeoutMinutes  int64 `json:"compose_dependency_wait_timeout_minutes"`
	ComposeCoordinatorIntervalSeconds    int64 `json:"compose_coordinator_interval_seconds"`
	VolumeUsageIntervalSeconds           int64 `json:"volume_usage_interval_seconds"`
	VolumeUsageCycleBudgetSeconds        int64 `json:"volume_usage_cycle_budget_seconds"`
	VolumeUsageScanEntryBudget           int   `json:"volume_usage_scan_entry_budget"`
	RegistryCleanupIntervalSeconds       int64 `json:"registry_cleanup_interval_seconds"`
	RegistryCleanupJobTimeoutSeconds     int64 `json:"registry_cleanup_job_timeout_seconds"`
	// RegistryGCMaxDurationSeconds is the Registry garbage collection lease:
	// no build is claimed until collection finishes or the lease expires.
	RegistryGCMaxDurationSeconds  int64 `json:"registry_gc_max_duration_seconds"`
	HostMinFreeDiskBytes          int64 `json:"host_min_free_disk_bytes"`
	MemoryPressureMinFreeBytes    int64 `json:"memory_pressure_min_free_bytes"`
	MemoryPressureIntervalSeconds int64 `json:"memory_pressure_interval_seconds"`
	// MemoryBoostMultiplier times the memory reservation is the boost
	// ceiling a running container's limit may be raised to. At or below
	// MaxBurstMultiplier it turns memory boost off.
	MemoryBoostMultiplier int64 `json:"memory_boost_multiplier"`
	// CPUBoostMultiplier times the CPU reservation is the boost ceiling a
	// running container's CPU quota may be raised to while the host CPU is
	// idle. At or below MaxCPUBurstMultiplier it turns CPU boost off.
	CPUBoostMultiplier int64 `json:"cpu_boost_multiplier"`
	// BoostMinFreeMemoryBytes is the free part of the user memory pool that
	// must remain after a boost is granted. Below it boosted limits shrink.
	BoostMinFreeMemoryBytes int64 `json:"boost_min_free_memory_bytes"`
	// DefaultVolumeSizeBytes is the size of a volume created without one,
	// including every volume a Compose deployment creates.
	DefaultVolumeSizeBytes int64 `json:"default_volume_size_bytes"`
	// VolumeSizeEnforcement gives each new volume's size to Docker so the
	// kernel refuses writes past it. Turning it on requires a host whose
	// Docker data directory supports project quotas.
	VolumeSizeEnforcement bool `json:"volume_size_enforcement"`
	// DefaultUserQuotaCPU, DefaultUserQuotaRAMMB and DefaultUserQuotaDiskMB
	// are the quota of every user without a custom quota, in cores and MB.
	// They are read at use, so a change applies at each such user's next
	// admission.
	DefaultUserQuotaCPU    float64 `json:"default_user_quota_cpu"`
	DefaultUserQuotaRAMMB  int64   `json:"default_user_quota_ram_mb"`
	DefaultUserQuotaDiskMB int64   `json:"default_user_quota_disk_mb"`

	GitSourcesEnabled      bool     `json:"git_sources_enabled"`
	GitAllowedHosts        []string `json:"git_allowed_hosts"`
	GitCloneTimeoutSeconds int64    `json:"git_clone_timeout_seconds"`
	GitMaxRepositoryBytes  int64    `json:"git_max_repository_bytes"`

	TelemetryMaxLogTailLines            int      `json:"telemetry_max_log_tail_lines"`
	TelemetryMaxLogStreamsPerUser       int      `json:"telemetry_max_log_streams_per_user"`
	TelemetryMaxTerminalSessionsPerUser int      `json:"telemetry_max_terminal_sessions_per_user"`
	TelemetryTerminalIdleTimeoutSeconds int64    `json:"telemetry_terminal_idle_timeout_seconds"`
	TelemetryTerminalMaxDurationSeconds int64    `json:"telemetry_terminal_max_duration_seconds"`
	TelemetryAllowedExecCommands        []string `json:"telemetry_allowed_exec_commands"`
	TelemetryMaxCommandArgs             int      `json:"telemetry_max_command_args"`
	TelemetryMaxCommandArgBytes         int      `json:"telemetry_max_command_arg_bytes"`
	TelemetryWSReadLimitBytes           int64    `json:"telemetry_ws_read_limit_bytes"`
}
