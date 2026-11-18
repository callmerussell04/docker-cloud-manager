package config

import (
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/imageref"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
)

// Settings separates the two lifetimes of Core's configuration.
type Settings struct {
	// Bootstrap holds secrets, DSNs, service targets, ports and storage settings.
	// They come only from the environment and never become runtime configuration.
	Bootstrap Bootstrap
	// Runtime seeds the Manager: its runtime settings are the defaults of a new
	// config file, which admins then change through /admin/config, and its Fixed
	// part is what every snapshot carries until Core restarts.
	Runtime SystemConfig
}

// Bootstrap is Core's process-lifetime configuration.
type Bootstrap struct {
	Port        int
	HTTPPort    int
	DBURL       string
	RabbitMQURL string
	SSOTarget   string
	// ServiceToken is Core's own credential, sent with its calls to SSO.
	ServiceToken string
	// GatewayToken and BuilderToken identify the callers Core accepts.
	GatewayToken string
	BuilderToken string
	HostDiskPath string
	// TenantCgroupParent is the cgroup Docker places user containers under.
	// Empty keeps Docker's default parent.
	TenantCgroupParent string
	ObjectStorage      objectstorage.Config
	ClickHouse         ClickHouse
	Registry           Registry
	// HTTP configures the internal HTTP listener, which also serves the
	// long-lived container log streams and terminal sessions.
	HTTP    HTTPServer
	Streams StreamLimits
}

// HTTPServer configures Core's internal HTTP listener. It sets no read or
// write timeout, because log streams, terminal sessions and uploads outlive any.
type HTTPServer struct {
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
}

// StreamLimits are the global limits on open container streams across all
// users. The per-user limits are runtime settings.
type StreamLimits struct {
	MaxLogStreams       int
	MaxTerminalSessions int
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
		DBURL:        dbURL,
		RabbitMQURL:  required.Get("RABBITMQ_URL"),
		SSOTarget:    required.Get("SSO_GRPC_TARGET"),
		ServiceToken: required.Get("CORE_SERVICE_TOKEN"),
		GatewayToken: required.Get("GATEWAY_SERVICE_TOKEN"),
		BuilderToken: required.Get("BUILDER_SERVICE_TOKEN"),
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
		HTTP: HTTPServer{
			ReadHeaderTimeout: envconfig.DurationSeconds(lookup, "TELEMETRY_READ_HEADER_TIMEOUT_SECONDS", 5*time.Second),
			IdleTimeout:       envconfig.DurationSeconds(lookup, "TELEMETRY_IDLE_TIMEOUT_SECONDS", 120*time.Second),
			ShutdownTimeout:   envconfig.DurationSeconds(lookup, "TELEMETRY_SHUTDOWN_TIMEOUT_SECONDS", 15*time.Second),
			MaxHeaderBytes:    envconfig.Int(lookup, "TELEMETRY_MAX_HEADER_BYTES", 1<<20),
		},
		Streams: StreamLimits{
			MaxLogStreams:       envconfig.Int(lookup, "TELEMETRY_MAX_GLOBAL_LOG_STREAMS", 100),
			MaxTerminalSessions: envconfig.Int(lookup, "TELEMETRY_MAX_GLOBAL_TERMINAL_SESSIONS", 50),
		},
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
	if err := envconfig.Distinct(
		envconfig.Setting{Key: "CORE_SERVICE_TOKEN", Value: bootstrap.ServiceToken},
		envconfig.Setting{Key: "GATEWAY_SERVICE_TOKEN", Value: bootstrap.GatewayToken},
		envconfig.Setting{Key: "BUILDER_SERVICE_TOKEN", Value: bootstrap.BuilderToken},
	); err != nil {
		return Settings{}, err
	}
	return Settings{Bootstrap: bootstrap, Runtime: loadRuntime(lookup, baseDomain)}, nil
}

// loadRuntime reads the runtime settings a new config file starts from, and
// the fixed part every snapshot carries.
func loadRuntime(lookup envconfig.Lookup, baseDomain string) SystemConfig {
	return SystemConfig{
		ReservedSystemMemory: envconfig.Int64(lookup, "RESERVED_SYSTEM_MEMORY_BYTES", 2<<30),
		OvercommitFactor:     envconfig.Float64(lookup, "OVERCOMMIT_FACTOR", 1),
		ReservedSystemCPU:    envconfig.Int64(lookup, "RESERVED_SYSTEM_CPU_MILLICORES", 500),
		CPUOvercommitFactor:  envconfig.Float64(lookup, "CPU_OVERCOMMIT_FACTOR", 4),
		HostMinFreeDiskBytes: envconfig.Int64(lookup, "HOST_MIN_FREE_DISK_BYTES", 1<<30),

		DefaultMemoryReservation:   envconfig.Int64(lookup, "DEFAULT_MEMORY_RESERVATION_BYTES", 256<<20),
		MaxBurstMultiplier:         envconfig.Int64(lookup, "MAX_BURST_MULTIPLIER", 4),
		DefaultCPUReservation:      envconfig.Int64(lookup, "DEFAULT_CPU_RESERVATION_MILLICORES", 250),
		MaxCPUBurstMultiplier:      envconfig.Int64(lookup, "MAX_CPU_BURST_MULTIPLIER", 4),
		MemoryPressureMinFreeBytes: envconfig.Int64(lookup, "MEMORY_PRESSURE_MIN_FREE_BYTES", 512<<20),
		MemoryBoostMultiplier:      envconfig.Int64(lookup, "MEMORY_BOOST_MULTIPLIER", 8),
		CPUBoostMultiplier:         envconfig.Int64(lookup, "CPU_BOOST_MULTIPLIER", 8),
		BoostMinFreeMemoryBytes:    envconfig.Int64(lookup, "BOOST_MIN_FREE_MEMORY_BYTES", 2<<30),

		ContainerTTLHours:      envconfig.Int64(lookup, "CONTAINER_TTL_HOURS", 24),
		ContainerStopTimeout:   envconfig.Int(lookup, "CONTAINER_STOP_TIMEOUT", 10),
		ContainerPidsLimit:     envconfig.Int64(lookup, "CONTAINER_PIDS_LIMIT", 256),
		ContainerDiskQuota:     envconfig.String(lookup, "CONTAINER_DISK_QUOTA", "1G"),
		MaxLogSize:             envconfig.String(lookup, "MAX_LOG_SIZE", "10m"),
		DefaultVolumeSizeBytes: envconfig.Int64(lookup, "DEFAULT_VOLUME_SIZE_BYTES", 256<<20),
		VolumeSizeEnforcement:  envconfig.Bool(lookup, "VOLUME_SIZE_ENFORCEMENT", false),

		DefaultUserQuotaCPU:            envconfig.Float64(lookup, "DEFAULT_USER_QUOTA_CPU", 2),
		DefaultUserQuotaRAMMB:          envconfig.Int64(lookup, "DEFAULT_USER_QUOTA_RAM_MB", 2048),
		DefaultUserQuotaDiskMB:         envconfig.Int64(lookup, "DEFAULT_USER_QUOTA_DISK_MB", 5120),
		MaxContainersPerUser:           envconfig.Int(lookup, "MAX_CONTAINERS_PER_USER", 10),
		MaxVolumesPerUser:              envconfig.Int(lookup, "MAX_VOLUMES_PER_USER", 5),
		MaxQueuedBuildsPerUser:         envconfig.Int(lookup, "MAX_QUEUED_BUILDS_PER_USER", 10),
		MaxQueuedComposeDeploysPerUser: envconfig.Int(lookup, "MAX_QUEUED_COMPOSE_DEPLOYS_PER_USER", 5),
		MaxStagedSourceBytesPerUser:    envconfig.Int64(lookup, "MAX_STAGED_SOURCE_BYTES_PER_USER", 1<<30),

		ImageBuildsEnabled:   envconfig.Bool(lookup, "IMAGE_BUILDS_ENABLED", true),
		MaxConcurrentBuilds:  envconfig.Int(lookup, "MAX_CONCURRENT_BUILDS", 2),
		BuildMemoryBytes:     envconfig.Int64(lookup, "BUILD_MEMORY_BYTES", 512<<20),
		BuildCPUMillicores:   envconfig.Int64(lookup, "BUILD_CPU_MILLICORES", 1000),
		BuildMaxDiskBytes:    envconfig.Int64(lookup, "BUILD_MAX_DISK_BYTES", DefaultBuildMaxDiskBytes),
		MaxBuildTimeMinutes:  envconfig.Int64(lookup, "MAX_BUILD_TIME_MINUTES", 10),
		MaxBuildLogSizeBytes: envconfig.Int64(lookup, "MAX_BUILD_LOG_SIZE_BYTES", 5<<20),
		MaxArchiveSizeBytes:  envconfig.Int64(lookup, "MAX_ARCHIVE_SIZE_BYTES", 50<<20),
		MaxUnpackedSizeBytes: envconfig.Int64(lookup, "MAX_UNPACKED_SIZE_BYTES", 500<<20),

		GitSourcesEnabled:      envconfig.Bool(lookup, "GIT_SOURCES_ENABLED", true),
		GitAllowedHosts:        envconfig.NonEmptyCSV(lookup, "GIT_ALLOWED_HOSTS", []string{"github.com", "gitlab.com", "bitbucket.org"}),
		GitCloneTimeoutSeconds: envconfig.Int64(lookup, "GIT_CLONE_TIMEOUT_SECONDS", 60),
		GitMaxRepositoryBytes:  envconfig.Int64(lookup, "GIT_MAX_REPOSITORY_BYTES", 200<<20),

		ComposeUploadMaxBytes:               envconfig.Int64(lookup, "COMPOSE_UPLOAD_MAX_BYTES", 100<<20),
		ComposePipelineTimeoutMinutes:       envconfig.Int64(lookup, "COMPOSE_PIPELINE_TIMEOUT_MINUTES", 30),
		ComposeDependencyWaitTimeoutMinutes: envconfig.Int64(lookup, "COMPOSE_DEPENDENCY_WAIT_TIMEOUT_MINUTES", 5),

		ReservedDomainPrefixes:      envconfig.NonEmptyCSV(lookup, "RESERVED_DOMAIN_PREFIXES", []string{"api", "admin", "gateway", "sso", "core", "builder", "traefik", "registry"}),
		BlockedDomainPrefixPatterns: []string{},

		TelemetryAllowedExecCommands:        envconfig.NonEmptyCSV(lookup, "TELEMETRY_ALLOWED_EXEC_COMMANDS", []string{"/bin/sh", "/bin/bash", "/busybox/sh"}),
		TelemetryTerminalIdleTimeoutSeconds: envconfig.Int64(lookup, "TELEMETRY_TERMINAL_IDLE_TIMEOUT_SECONDS", 300),
		TelemetryTerminalMaxDurationSeconds: envconfig.Int64(lookup, "TELEMETRY_TERMINAL_MAX_DURATION_SECONDS", 3600),

		Fixed: loadFixed(lookup, baseDomain),
	}
}

// loadFixed reads the part of the snapshot that only a restart changes.
func loadFixed(lookup envconfig.Lookup, baseDomain string) Fixed {
	return Fixed{
		BaseDomain:            baseDomain,
		RegistryAPIURL:        envconfig.String(lookup, "REGISTRY_API_URL", "https://registry-backend:5000"),
		RegistryPublicURL:     envconfig.String(lookup, "REGISTRY_PUBLIC_URL", "registry:5000"),
		RegistryContainerName: envconfig.String(lookup, "REGISTRY_CONTAINER_NAME", "registry"),
		ProxyNetworkName:      envconfig.String(lookup, "PROXY_NETWORK_NAME", "proxy_net"),
		BuildNetworkName:      envconfig.String(lookup, "BUILD_NETWORK_NAME", "build_net"),
		KanikoImage:           envconfig.String(lookup, "KANIKO_IMAGE", imageref.DefaultKaniko),

		ContainerCPUPeriod:               envconfig.Int64(lookup, "CONTAINER_CPU_PERIOD", 100000),
		DefaultCPUShares:                 envconfig.Int64(lookup, "DEFAULT_CPU_SHARES", 1024),
		ContainerMemorySwapMultiplier:    envconfig.Float64(lookup, "CONTAINER_MEMORY_SWAP_MULTIPLIER", 2),
		MaxLogFiles:                      envconfig.String(lookup, "MAX_LOG_FILES", "3"),
		MaxQueuedContainerCreatesPerUser: envconfig.Int(lookup, "MAX_QUEUED_CONTAINER_CREATES_PER_USER", 5),

		BuildCPUPeriod:                 envconfig.Int64(lookup, "BUILD_CPU_PERIOD", 100000),
		BuildMemorySwapMultiplier:      envconfig.Float64(lookup, "BUILD_MEMORY_SWAP_MULTIPLIER", 2),
		BuildPidsLimit:                 envconfig.Int64(lookup, "BUILD_PIDS_LIMIT", 512),
		BuildCancelPollIntervalSeconds: envconfig.Int64(lookup, "BUILD_CANCEL_POLL_INTERVAL_SECONDS", 2),

		ContainerCreateWorkerCount:           envconfig.Int(lookup, "CONTAINER_CREATE_WORKER_COUNT", 2),
		ContainerCreateMaxAttempts:           envconfig.Int(lookup, "CONTAINER_CREATE_MAX_ATTEMPTS", 3),
		ContainerCreateTimeoutMinutes:        envconfig.Int64(lookup, "CONTAINER_CREATE_TIMEOUT_MINUTES", 30),
		ContainerCreateOutboxIntervalSeconds: envconfig.Int64(lookup, "CONTAINER_CREATE_OUTBOX_INTERVAL_SECONDS", 1),
		ContainerCreateOutboxBatchSize:       envconfig.Int(lookup, "CONTAINER_CREATE_OUTBOX_BATCH_SIZE", 10),
		BuildOutboxIntervalSeconds:           envconfig.Int64(lookup, "BUILD_OUTBOX_INTERVAL_SECONDS", 1),
		BuildOutboxBatchSize:                 envconfig.Int(lookup, "BUILD_OUTBOX_BATCH_SIZE", 10),
		ComposeDeployWorkerCount:             envconfig.Int(lookup, "COMPOSE_DEPLOY_WORKER_COUNT", 2),
		ComposeDeployMaxAttempts:             envconfig.Int(lookup, "COMPOSE_DEPLOY_MAX_ATTEMPTS", 3),
		ComposeOutboxIntervalSeconds:         envconfig.Int64(lookup, "COMPOSE_OUTBOX_INTERVAL_SECONDS", 1),
		ComposeOutboxBatchSize:               envconfig.Int(lookup, "COMPOSE_OUTBOX_BATCH_SIZE", 10),
		ComposeCoordinatorIntervalSeconds:    envconfig.Int64(lookup, "COMPOSE_COORDINATOR_INTERVAL_SECONDS", 2),

		TTLWorkerIntervalSeconds:            envconfig.Int64(lookup, "TTL_WORKER_INTERVAL_SECONDS", 60),
		EventSyncIntervalSeconds:            envconfig.Int64(lookup, "EVENT_SYNC_INTERVAL_SECONDS", 30),
		EventReconnectDelaySeconds:          envconfig.Int64(lookup, "EVENT_RECONNECT_DELAY_SECONDS", 5),
		GCWorkerIntervalMinutes:             envconfig.Int64(lookup, "GC_WORKER_INTERVAL_MINUTES", 60),
		StaleBuildTimeoutMinutes:            envconfig.Int64(lookup, "STALE_BUILD_TIMEOUT_MINUTES", 30),
		RegistryCleanupIntervalSeconds:      envconfig.Int64(lookup, "REGISTRY_CLEANUP_INTERVAL_SECONDS", 5),
		RegistryCleanupJobTimeoutSeconds:    envconfig.Int64(lookup, "REGISTRY_CLEANUP_JOB_TIMEOUT_SECONDS", 20),
		RegistryGCMaxDurationSeconds:        envconfig.Int64(lookup, "REGISTRY_GC_MAX_DURATION_SECONDS", 900),
		VolumeUsageIntervalSeconds:          envconfig.Int64(lookup, "VOLUME_USAGE_INTERVAL_SECONDS", 60),
		VolumeUsageCycleBudgetSeconds:       envconfig.Int64(lookup, "VOLUME_USAGE_CYCLE_BUDGET_SECONDS", 15),
		VolumeUsageScanEntryBudget:          envconfig.Int(lookup, "VOLUME_USAGE_SCAN_ENTRY_BUDGET", 50000),
		MemoryPressureIntervalSeconds:       envconfig.Int64(lookup, "MEMORY_PRESSURE_INTERVAL_SECONDS", 10),
		ReportsUsageSnapshotIntervalSeconds: envconfig.Int64(lookup, "REPORTS_USAGE_SNAPSHOT_INTERVAL_SECONDS", DefaultReportsUsageSnapshotIntervalSeconds),

		TelemetryMaxLogTailLines:            envconfig.Int(lookup, "TELEMETRY_MAX_LOG_TAIL_LINES", 1000),
		TelemetryMaxLogStreamsPerUser:       envconfig.Int(lookup, "TELEMETRY_MAX_LOG_STREAMS_PER_USER", 5),
		TelemetryMaxTerminalSessionsPerUser: envconfig.Int(lookup, "TELEMETRY_MAX_TERMINAL_SESSIONS_PER_USER", 2),
		TelemetryMaxCommandArgs:             envconfig.Int(lookup, "TELEMETRY_MAX_COMMAND_ARGS", 8),
		TelemetryMaxCommandArgBytes:         envconfig.Int(lookup, "TELEMETRY_MAX_COMMAND_ARG_BYTES", 128),
		TelemetryWSReadLimitBytes:           envconfig.Int64(lookup, "TELEMETRY_WS_READ_LIMIT_BYTES", 4096),
	}
}
