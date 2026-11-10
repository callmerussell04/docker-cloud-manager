package config

import (
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/imageref"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
)

// Settings separates the two lifetimes of Core's configuration.
type Settings struct {
	// Bootstrap holds secrets, DSNs, service targets, ports and storage settings.
	// They come only from the environment and never become runtime configuration.
	Bootstrap Bootstrap
	// Runtime seeds the Manager's defaults; admins change it through /admin/config.
	// The Manager keeps its Registry and network name fields pinned to these values.
	Runtime SystemConfig
}

// Bootstrap is Core's process-lifetime configuration.
type Bootstrap struct {
	Port          int
	HTTPPort      int
	DBURL         string
	RabbitMQURL   string
	SSOTarget     string
	InternalToken string
	HostDiskPath  string
	// TenantCgroupParent is the cgroup Docker places user containers under.
	// Empty keeps Docker's default parent.
	TenantCgroupParent string
	ObjectStorage      objectstorage.Config
	ClickHouse         ClickHouse
	Registry           Registry
}

// ClickHouse locates the reports store. An empty Addr disables it.
type ClickHouse struct {
	Addr     string
	Database string
	Username string
	Password string
}

// Registry is bootstrap-only. The TLS edge has no Core control API routes.
type Registry struct {
	Authority              string
	BackendURL             string
	CAFile                 string
	CertificateFile        string
	KeyFile                string
	SigningCertificateFile string
	SigningKeyFile         string
	Issuer                 string
}

// Load reads Core's settings. Its error names the first missing required setting.
func Load(lookup envconfig.Lookup) (Settings, error) {
	required := envconfig.NewRequired(lookup)
	dbURL := required.Get("DATABASE_URL")
	baseDomain := required.Get("BASE_DOMAIN")

	bootstrap := Bootstrap{
		DBURL:         dbURL,
		RabbitMQURL:   required.Get("RABBITMQ_URL"),
		SSOTarget:     required.Get("SSO_GRPC_TARGET"),
		InternalToken: required.Get("INTERNAL_SERVICE_TOKEN"),
		Registry: Registry{
			Authority:              required.Get("REGISTRY_URL"),
			BackendURL:             required.Get("REGISTRY_BACKEND_URL"),
			CAFile:                 required.Get("REGISTRY_CA_FILE"),
			CertificateFile:        required.Get("REGISTRY_TLS_CERT_FILE"),
			KeyFile:                required.Get("REGISTRY_TLS_KEY_FILE"),
			SigningCertificateFile: required.Get("REGISTRY_SIGNING_CERT_FILE"),
			SigningKeyFile:         required.Get("REGISTRY_SIGNING_KEY_FILE"),
			Issuer:                 required.Get("REGISTRY_TOKEN_ISSUER"),
		},
		ObjectStorage: objectstorage.LoadConfig(lookup, required),
		Port:          envconfig.Int(lookup, "CORE_GRPC_PORT", 50052),
		HTTPPort:      envconfig.Int(lookup, "CORE_HTTP_PORT", 8083),
		HostDiskPath:  envconfig.String(lookup, "CORE_HOST_DISK_PATH", "/"),

		TenantCgroupParent: envconfig.String(lookup, "TENANT_CGROUP_PARENT", ""),
		ClickHouse: ClickHouse{
			Addr:     envconfig.String(lookup, "CLICKHOUSE_ADDR", ""),
			Database: envconfig.String(lookup, "CLICKHOUSE_DB", "dcm_reports"),
			Username: envconfig.String(lookup, "CLICKHOUSE_USER", ""),
			Password: envconfig.String(lookup, "CLICKHOUSE_PASSWORD", ""),
		},
	}
	if err := required.Err(); err != nil {
		return Settings{}, err
	}
	return Settings{Bootstrap: bootstrap, Runtime: loadRuntime(lookup, baseDomain)}, nil
}

// loadRuntime reads the defaults a new runtime config file starts from.
func loadRuntime(lookup envconfig.Lookup, baseDomain string) SystemConfig {
	return SystemConfig{
		BaseDomain:                           baseDomain,
		DefaultMemoryReservation:             envconfig.Int64(lookup, "DEFAULT_MEMORY_RESERVATION_BYTES", 256*1024*1024), // 256 MB
		ReservedSystemMemory:                 envconfig.Int64(lookup, "RESERVED_SYSTEM_MEMORY_BYTES", 2*1024*1024*1024),  // 2 GB
		OvercommitFactor:                     envconfig.Float64(lookup, "OVERCOMMIT_FACTOR", 1),
		MaxBurstMultiplier:                   envconfig.Int64(lookup, "MAX_BURST_MULTIPLIER", 4),
		DefaultCPUReservation:                envconfig.Int64(lookup, "DEFAULT_CPU_RESERVATION_MILLICORES", 250),
		ReservedSystemCPU:                    envconfig.Int64(lookup, "RESERVED_SYSTEM_CPU_MILLICORES", 500),
		CPUOvercommitFactor:                  envconfig.Float64(lookup, "CPU_OVERCOMMIT_FACTOR", 4),
		MaxCPUBurstMultiplier:                envconfig.Int64(lookup, "MAX_CPU_BURST_MULTIPLIER", 4),
		ContainerCPUPeriod:                   envconfig.Int64(lookup, "CONTAINER_CPU_PERIOD", 100000),
		DefaultCPUShares:                     envconfig.Int64(lookup, "DEFAULT_CPU_SHARES", 1024),
		ContainerStopTimeout:                 envconfig.Int(lookup, "CONTAINER_STOP_TIMEOUT", 10),
		MaxLogSize:                           envconfig.String(lookup, "MAX_LOG_SIZE", "10m"),
		MaxLogFiles:                          envconfig.String(lookup, "MAX_LOG_FILES", "3"),
		ContainerDiskQuota:                   envconfig.String(lookup, "CONTAINER_DISK_QUOTA", "1G"),
		ReservedDomainPrefixes:               envconfig.NonEmptyCSV(lookup, "RESERVED_DOMAIN_PREFIXES", []string{"api", "admin", "gateway", "sso", "core", "builder", "traefik", "registry"}),
		BlockedDomainPrefixPatterns:          []string{},
		MaxVolumesPerUser:                    envconfig.Int(lookup, "MAX_VOLUMES_PER_USER", 5),
		MaxContainersPerUser:                 envconfig.Int(lookup, "MAX_CONTAINERS_PER_USER", 10),
		RegistryAPIURL:                       envconfig.String(lookup, "REGISTRY_API_URL", "https://registry-backend:5000"),
		RegistryPublicURL:                    envconfig.String(lookup, "REGISTRY_PUBLIC_URL", "registry:5000"),
		ContainerTTLHours:                    envconfig.Int64(lookup, "CONTAINER_TTL_HOURS", 24),
		ContainerPidsLimit:                   envconfig.Int64(lookup, "CONTAINER_PIDS_LIMIT", 256),
		ContainerMemorySwapMultiplier:        envconfig.Float64(lookup, "CONTAINER_MEMORY_SWAP_MULTIPLIER", 2),
		ProxyNetworkName:                     envconfig.String(lookup, "PROXY_NETWORK_NAME", "proxy_net"),
		RegistryContainerName:                envconfig.String(lookup, "REGISTRY_CONTAINER_NAME", "registry"),
		ImageBuildsEnabled:                   envconfig.Bool(lookup, "IMAGE_BUILDS_ENABLED", true),
		BuildMemoryBytes:                     envconfig.Int64(lookup, "BUILD_MEMORY_BYTES", 512*1024*1024),
		BuildCPUQuota:                        envconfig.Int64(lookup, "BUILD_CPU_QUOTA", 100000),
		BuildCPUPeriod:                       envconfig.Int64(lookup, "BUILD_CPU_PERIOD", 100000),
		BuildMemorySwapMultiplier:            envconfig.Float64(lookup, "BUILD_MEMORY_SWAP_MULTIPLIER", 2),
		BuildPidsLimit:                       envconfig.Int64(lookup, "BUILD_PIDS_LIMIT", 512),
		BuildNetworkName:                     envconfig.String(lookup, "BUILD_NETWORK_NAME", "build_net"),
		KanikoImage:                          envconfig.String(lookup, "KANIKO_IMAGE", imageref.DefaultKaniko),
		MaxBuildTimeMinutes:                  envconfig.Int64(lookup, "MAX_BUILD_TIME_MINUTES", 10),
		MaxConcurrentBuilds:                  envconfig.Int(lookup, "MAX_CONCURRENT_BUILDS", 2),
		MaxUploadSizeBytes:                   envconfig.Int64(lookup, "MAX_UPLOAD_SIZE_BYTES", 50<<20),
		MaxArchiveSizeBytes:                  envconfig.Int64(lookup, "MAX_ARCHIVE_SIZE_BYTES", 50<<20),
		MaxUnpackedSizeBytes:                 envconfig.Int64(lookup, "MAX_UNPACKED_SIZE_BYTES", 500*1024*1024),
		MaxBuildLogSizeBytes:                 envconfig.Int64(lookup, "MAX_BUILD_LOG_SIZE_BYTES", 5*1024*1024),
		BuildCancelPollIntervalSeconds:       envconfig.Int64(lookup, "BUILD_CANCEL_POLL_INTERVAL_SECONDS", 2),
		TTLWorkerIntervalSeconds:             envconfig.Int64(lookup, "TTL_WORKER_INTERVAL_SECONDS", 60),
		GCWorkerIntervalMinutes:              envconfig.Int64(lookup, "GC_WORKER_INTERVAL_MINUTES", 60),
		StaleBuildTimeoutMinutes:             envconfig.Int64(lookup, "STALE_BUILD_TIMEOUT_MINUTES", 30),
		EventSyncIntervalSeconds:             envconfig.Int64(lookup, "EVENT_SYNC_INTERVAL_SECONDS", 30),
		EventReconnectDelaySeconds:           envconfig.Int64(lookup, "EVENT_RECONNECT_DELAY_SECONDS", 5),
		BuildOutboxIntervalSeconds:           envconfig.Int64(lookup, "BUILD_OUTBOX_INTERVAL_SECONDS", 1),
		BuildOutboxBatchSize:                 envconfig.Int(lookup, "BUILD_OUTBOX_BATCH_SIZE", 10),
		ContainerCreateWorkerCount:           envconfig.Int(lookup, "CONTAINER_CREATE_WORKER_COUNT", 2),
		ContainerCreateMaxAttempts:           envconfig.Int(lookup, "CONTAINER_CREATE_MAX_ATTEMPTS", 3),
		ContainerCreateTimeoutMinutes:        envconfig.Int64(lookup, "CONTAINER_CREATE_TIMEOUT_MINUTES", 30),
		MaxQueuedContainerCreatesPerUser:     envconfig.Int(lookup, "MAX_QUEUED_CONTAINER_CREATES_PER_USER", 5),
		ContainerCreateOutboxIntervalSeconds: envconfig.Int64(lookup, "CONTAINER_CREATE_OUTBOX_INTERVAL_SECONDS", 1),
		ContainerCreateOutboxBatchSize:       envconfig.Int(lookup, "CONTAINER_CREATE_OUTBOX_BATCH_SIZE", 10),
		ReportsUsageSnapshotIntervalSeconds:  envconfig.Int64(lookup, "REPORTS_USAGE_SNAPSHOT_INTERVAL_SECONDS", DefaultReportsUsageSnapshotIntervalSeconds),
		MaxStagedSourceBytesPerUser:          envconfig.Int64(lookup, "MAX_STAGED_SOURCE_BYTES_PER_USER", 1024*1024*1024),
		MaxQueuedBuildsPerUser:               envconfig.Int(lookup, "MAX_QUEUED_BUILDS_PER_USER", 10),
		ComposeUploadMaxBytes:                envconfig.Int64(lookup, "COMPOSE_UPLOAD_MAX_BYTES", 100<<20),
		ComposePipelineTimeoutMinutes:        envconfig.Int64(lookup, "COMPOSE_PIPELINE_TIMEOUT_MINUTES", 30),
		ComposeDeployWorkerCount:             envconfig.Int(lookup, "COMPOSE_DEPLOY_WORKER_COUNT", 2),
		ComposeOutboxIntervalSeconds:         envconfig.Int64(lookup, "COMPOSE_OUTBOX_INTERVAL_SECONDS", 1),
		ComposeOutboxBatchSize:               envconfig.Int(lookup, "COMPOSE_OUTBOX_BATCH_SIZE", 10),
		ComposeDeployMaxAttempts:             envconfig.Int(lookup, "COMPOSE_DEPLOY_MAX_ATTEMPTS", 3),
		MaxQueuedComposeDeploysPerUser:       envconfig.Int(lookup, "MAX_QUEUED_COMPOSE_DEPLOYS_PER_USER", 5),
		ComposeBuildPollIntervalSeconds:      envconfig.Int64(lookup, "COMPOSE_BUILD_POLL_INTERVAL_SECONDS", 3),
		ComposeDependencyWaitTimeoutMinutes:  envconfig.Int64(lookup, "COMPOSE_DEPENDENCY_WAIT_TIMEOUT_MINUTES", 5),
		ComposeCoordinatorIntervalSeconds:    envconfig.Int64(lookup, "COMPOSE_COORDINATOR_INTERVAL_SECONDS", 2),
		VolumeUsageIntervalSeconds:           envconfig.Int64(lookup, "VOLUME_USAGE_INTERVAL_SECONDS", 60),
		VolumeUsageCycleBudgetSeconds:        envconfig.Int64(lookup, "VOLUME_USAGE_CYCLE_BUDGET_SECONDS", 15),
		VolumeUsageScanEntryBudget:           envconfig.Int(lookup, "VOLUME_USAGE_SCAN_ENTRY_BUDGET", 50000),
		RegistryCleanupIntervalSeconds:       envconfig.Int64(lookup, "REGISTRY_CLEANUP_INTERVAL_SECONDS", 5),
		RegistryCleanupJobTimeoutSeconds:     envconfig.Int64(lookup, "REGISTRY_CLEANUP_JOB_TIMEOUT_SECONDS", 20),
		HostMinFreeDiskBytes:                 envconfig.Int64(lookup, "HOST_MIN_FREE_DISK_BYTES", 1024*1024*1024),
		MemoryPressureMinFreeBytes:           envconfig.Int64(lookup, "MEMORY_PRESSURE_MIN_FREE_BYTES", 512*1024*1024),
		MemoryPressureIntervalSeconds:        envconfig.Int64(lookup, "MEMORY_PRESSURE_INTERVAL_SECONDS", 10),
		MemoryBoostMultiplier:                envconfig.Int64(lookup, "MEMORY_BOOST_MULTIPLIER", 8),
		BoostMinFreeMemoryBytes:              envconfig.Int64(lookup, "BOOST_MIN_FREE_MEMORY_BYTES", 2*1024*1024*1024),
		GitSourcesEnabled:                    envconfig.Bool(lookup, "GIT_SOURCES_ENABLED", true),
		GitAllowedHosts:                      envconfig.NonEmptyCSV(lookup, "GIT_ALLOWED_HOSTS", []string{"github.com", "gitlab.com", "bitbucket.org"}),
		GitCloneTimeoutSeconds:               envconfig.Int64(lookup, "GIT_CLONE_TIMEOUT_SECONDS", 60),
		GitMaxRepositoryBytes:                envconfig.Int64(lookup, "GIT_MAX_REPOSITORY_BYTES", 200*1024*1024),
		TelemetryMaxLogTailLines:             envconfig.Int(lookup, "TELEMETRY_MAX_LOG_TAIL_LINES", 1000),
		TelemetryMaxLogStreamsPerUser:        envconfig.Int(lookup, "TELEMETRY_MAX_LOG_STREAMS_PER_USER", 5),
		TelemetryMaxTerminalSessionsPerUser:  envconfig.Int(lookup, "TELEMETRY_MAX_TERMINAL_SESSIONS_PER_USER", 2),
		TelemetryTerminalIdleTimeoutSeconds:  envconfig.Int64(lookup, "TELEMETRY_TERMINAL_IDLE_TIMEOUT_SECONDS", 300),
		TelemetryTerminalMaxDurationSeconds:  envconfig.Int64(lookup, "TELEMETRY_TERMINAL_MAX_DURATION_SECONDS", 3600),
		TelemetryAllowedExecCommands:         envconfig.NonEmptyCSV(lookup, "TELEMETRY_ALLOWED_EXEC_COMMANDS", []string{"/bin/sh", "/bin/bash", "/busybox/sh"}),
		TelemetryMaxCommandArgs:              envconfig.Int(lookup, "TELEMETRY_MAX_COMMAND_ARGS", 8),
		TelemetryMaxCommandArgBytes:          envconfig.Int(lookup, "TELEMETRY_MAX_COMMAND_ARG_BYTES", 128),
		TelemetryWSReadLimitBytes:            envconfig.Int64(lookup, "TELEMETRY_WS_READ_LIMIT_BYTES", 4096),
	}
}
