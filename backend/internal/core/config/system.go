package config

type SystemConfig struct {
	BaseDomain               string   `json:"base_domain"`
	DefaultMemoryReservation int64    `json:"default_memory_reservation_bytes"`
	ReservedSystemMemory     int64    `json:"reserved_system_memory_bytes"`
	OvercommitFactor         float64  `json:"overcommit_factor"`
	MaxBurstMultiplier       int64    `json:"max_burst_multiplier"`
	DefaultCPUShares         int64    `json:"default_cpu_shares"`
	HighLoadCPUShares        int64    `json:"high_load_cpu_shares"`
	HighLoadContainerCount   int      `json:"high_load_container_count"`
	ContainerStopTimeout     int      `json:"container_stop_timeout"`
	MaxLogSize               string   `json:"max_log_size"`
	MaxLogFiles              string   `json:"max_log_files"`
	ContainerDiskQuota       string   `json:"container_disk_quota"`
	ReservedDomainPrefixes   []string `json:"reserved_domain_prefixes"`
	MaxVolumesPerUser        int      `json:"max_volumes_per_user"`
	MaxContainersPerUser     int      `json:"max_containers_per_user"`
	RegistryAPIURL           string   `json:"registry_api_url"`
	RegistryPublicURL        string   `json:"registry_public_url"`
	ContainerTTLHours        int64    `json:"container_ttl_hours"`

	ContainerPidsLimit            int64   `json:"container_pids_limit"`
	ContainerMemorySwapMultiplier float64 `json:"container_memory_swap_multiplier"`
	ProxyNetworkName              string  `json:"proxy_network_name"`
	RegistryContainerName         string  `json:"registry_container_name"`

	BuildMemoryBytes          int64   `json:"build_memory_bytes"`
	BuildCPUQuota             int64   `json:"build_cpu_quota"`
	BuildCPUPeriod            int64   `json:"build_cpu_period"`
	BuildMemorySwapMultiplier float64 `json:"build_memory_swap_multiplier"`
	BuildPidsLimit            int64   `json:"build_pids_limit"`
	BuildNetworkName          string  `json:"build_network_name"`
	KanikoImage               string  `json:"kaniko_image"`
	MaxBuildTimeMinutes       int64   `json:"max_build_time_minutes"`
	MaxConcurrentBuilds       int     `json:"max_concurrent_builds"`
	MaxUploadSizeBytes        int64   `json:"max_upload_size_bytes"`
	MaxArchiveSizeBytes       int64   `json:"max_archive_size_bytes"`
	MaxUnpackedSizeBytes      int64   `json:"max_unpacked_size_bytes"`
	MaxBuildLogSizeBytes      int64   `json:"max_build_log_size_bytes"`

	TTLWorkerIntervalSeconds             int64 `json:"ttl_worker_interval_seconds"`
	GCWorkerIntervalMinutes              int64 `json:"gc_worker_interval_minutes"`
	StaleBuildTimeoutMinutes             int64 `json:"stale_build_timeout_minutes"`
	EventSyncIntervalSeconds             int64 `json:"event_sync_interval_seconds"`
	EventReconnectDelaySeconds           int64 `json:"event_reconnect_delay_seconds"`
	BuildOutboxIntervalSeconds           int64 `json:"build_outbox_interval_seconds"`
	BuildOutboxBatchSize                 int   `json:"build_outbox_batch_size"`
	ComposeUploadMaxBytes                int64 `json:"compose_upload_max_bytes"`
	ComposePipelineTimeoutMinutes        int64 `json:"compose_pipeline_timeout_minutes"`
	ComposeBuilderHTTPTimeoutSeconds     int64 `json:"compose_builder_http_timeout_seconds"`
	ComposeBuildPollIntervalSeconds      int64 `json:"compose_build_poll_interval_seconds"`
	ComposeDependencyWaitTimeoutMinutes  int64 `json:"compose_dependency_wait_timeout_minutes"`
	ComposeDependencyPollIntervalSeconds int64 `json:"compose_dependency_poll_interval_seconds"`
}
