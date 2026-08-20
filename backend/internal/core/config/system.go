package config

const (
	DefaultReportsUsageSnapshotIntervalSeconds = int64(300)
	MinReportsUsageSnapshotIntervalSeconds     = int64(60)
	MaxReportsUsageSnapshotIntervalSeconds     = int64(86400)
)

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
	HighLoadCPUShares           int64    `json:"high_load_cpu_shares"`
	HighLoadContainerCount      int      `json:"high_load_container_count"`
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
	ComposeDependencyPollIntervalSeconds int64 `json:"compose_dependency_poll_interval_seconds"`
	ComposeCoordinatorIntervalSeconds    int64 `json:"compose_coordinator_interval_seconds"`
	HostMinFreeDiskBytes                 int64 `json:"host_min_free_disk_bytes"`

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
