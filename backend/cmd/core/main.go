package main

import (
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/app"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/callmerussell04/docker-cloud-manager/pkg/objectstorage"
	_ "github.com/lib/pq"
)

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvInt64(key string, fallback int64) int64 {
	if value, ok := os.LookupEnv(key); ok {
		if i, err := strconv.ParseInt(value, 10, 64); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if value, ok := os.LookupEnv(key); ok {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if value, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return fallback
}

func getEnvString(key string, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func requiredEnv(logger *slog.Logger, key string) string {
	value := os.Getenv(key)
	if value == "" {
		fatal(logger, "required environment variable is not set", "env_var", key)
	}
	return value
}

func getEnvStringList(key string, fallback []string) []string {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	if len(result) == 0 {
		return fallback
	}
	return result
}

func main() {
	logger := logging.NewLogger("core", logging.ConfigFromEnv())
	slog.SetDefault(logger)

	port := getEnvInt("CORE_GRPC_PORT", 50052)
	httpPort := getEnvInt("CORE_HTTP_PORT", 8083)
	hostDiskPath := getEnvString("CORE_HOST_DISK_PATH", "/")

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fatal(logger, "required environment variable is not set", "env_var", "DATABASE_URL")
	}

	baseDomain := os.Getenv("BASE_DOMAIN")
	if baseDomain == "" {
		fatal(logger, "required environment variable is not set", "env_var", "BASE_DOMAIN")
	}

	rabbitMQURL := os.Getenv("RABBITMQ_URL")
	if rabbitMQURL == "" {
		fatal(logger, "required environment variable is not set", "env_var", "RABBITMQ_URL")
	}

	ssoTarget := os.Getenv("SSO_GRPC_TARGET")
	if ssoTarget == "" {
		fatal(logger, "required environment variable is not set", "env_var", "SSO_GRPC_TARGET")
	}

	internalToken := os.Getenv("INTERNAL_SERVICE_TOKEN")
	if internalToken == "" {
		fatal(logger, "required environment variable is not set", "env_var", "INTERNAL_SERVICE_TOKEN")
	}

	registryAPIURL := getEnvString("REGISTRY_API_URL", "registry:5000")
	registryPublicURL := getEnvString("REGISTRY_PUBLIC_URL", "localhost:5000")
	registryContainerName := getEnvString("REGISTRY_CONTAINER_NAME", "registry")

	defaultCfg := config.SystemConfig{
		BaseDomain:                           baseDomain,
		DefaultMemoryReservation:             getEnvInt64("DEFAULT_MEMORY_RESERVATION_BYTES", 256*1024*1024), // 256 MB
		ReservedSystemMemory:                 getEnvInt64("RESERVED_SYSTEM_MEMORY_BYTES", 2*1024*1024*1024),  // 2 GB
		OvercommitFactor:                     getEnvFloat("OVERCOMMIT_FACTOR", 1.5),
		MaxBurstMultiplier:                   getEnvInt64("MAX_BURST_MULTIPLIER", 4),
		DefaultCPUShares:                     getEnvInt64("DEFAULT_CPU_SHARES", 1024),
		HighLoadCPUShares:                    getEnvInt64("HIGH_LOAD_CPU_SHARES", 512),
		HighLoadContainerCount:               getEnvInt("HIGH_LOAD_CONTAINER_COUNT", 5),
		ContainerStopTimeout:                 getEnvInt("CONTAINER_STOP_TIMEOUT", 10),
		MaxLogSize:                           getEnvString("MAX_LOG_SIZE", "10m"),
		MaxLogFiles:                          getEnvString("MAX_LOG_FILES", "3"),
		ContainerDiskQuota:                   getEnvString("CONTAINER_DISK_QUOTA", "1G"),
		ReservedDomainPrefixes:               getEnvStringList("RESERVED_DOMAIN_PREFIXES", []string{"api", "admin", "gateway", "sso", "core", "builder", "traefik", "registry"}),
		MaxVolumesPerUser:                    getEnvInt("MAX_VOLUMES_PER_USER", 5),
		MaxContainersPerUser:                 getEnvInt("MAX_CONTAINERS_PER_USER", 10),
		RegistryAPIURL:                       registryAPIURL,
		RegistryPublicURL:                    registryPublicURL,
		ContainerTTLHours:                    int64(getEnvInt("CONTAINER_TTL_HOURS", 24)),
		ContainerPidsLimit:                   getEnvInt64("CONTAINER_PIDS_LIMIT", 256),
		ContainerMemorySwapMultiplier:        getEnvFloat("CONTAINER_MEMORY_SWAP_MULTIPLIER", 2),
		ProxyNetworkName:                     getEnvString("PROXY_NETWORK_NAME", "proxy_net"),
		RegistryContainerName:                registryContainerName,
		ImageBuildsEnabled:                   getEnvBool("IMAGE_BUILDS_ENABLED", true),
		BuildMemoryBytes:                     getEnvInt64("BUILD_MEMORY_BYTES", 512*1024*1024),
		BuildCPUQuota:                        getEnvInt64("BUILD_CPU_QUOTA", 100000),
		BuildCPUPeriod:                       getEnvInt64("BUILD_CPU_PERIOD", 100000),
		BuildMemorySwapMultiplier:            getEnvFloat("BUILD_MEMORY_SWAP_MULTIPLIER", 2),
		BuildPidsLimit:                       getEnvInt64("BUILD_PIDS_LIMIT", 512),
		BuildNetworkName:                     getEnvString("BUILD_NETWORK_NAME", "build_net"),
		KanikoImage:                          getEnvString("KANIKO_IMAGE", "gcr.io/kaniko-project/executor:latest"),
		MaxBuildTimeMinutes:                  int64(getEnvInt("MAX_BUILD_TIME_MINUTES", 10)),
		MaxConcurrentBuilds:                  getEnvInt("MAX_CONCURRENT_BUILDS", 2),
		MaxUploadSizeBytes:                   getEnvInt64("MAX_UPLOAD_SIZE_BYTES", 50<<20),
		MaxArchiveSizeBytes:                  getEnvInt64("MAX_ARCHIVE_SIZE_BYTES", 50<<20),
		MaxUnpackedSizeBytes:                 getEnvInt64("MAX_UNPACKED_SIZE_BYTES", 500*1024*1024),
		MaxBuildLogSizeBytes:                 getEnvInt64("MAX_BUILD_LOG_SIZE_BYTES", 5*1024*1024),
		BuildCancelPollIntervalSeconds:       int64(getEnvInt("BUILD_CANCEL_POLL_INTERVAL_SECONDS", 2)),
		TTLWorkerIntervalSeconds:             int64(getEnvInt("TTL_WORKER_INTERVAL_SECONDS", 60)),
		GCWorkerIntervalMinutes:              int64(getEnvInt("GC_WORKER_INTERVAL_MINUTES", 60)),
		StaleBuildTimeoutMinutes:             int64(getEnvInt("STALE_BUILD_TIMEOUT_MINUTES", 30)),
		EventSyncIntervalSeconds:             int64(getEnvInt("EVENT_SYNC_INTERVAL_SECONDS", 30)),
		EventReconnectDelaySeconds:           int64(getEnvInt("EVENT_RECONNECT_DELAY_SECONDS", 5)),
		BuildOutboxIntervalSeconds:           int64(getEnvInt("BUILD_OUTBOX_INTERVAL_SECONDS", 1)),
		BuildOutboxBatchSize:                 getEnvInt("BUILD_OUTBOX_BATCH_SIZE", 10),
		ContainerCreateWorkerCount:           getEnvInt("CONTAINER_CREATE_WORKER_COUNT", 2),
		ContainerCreateMaxAttempts:           getEnvInt("CONTAINER_CREATE_MAX_ATTEMPTS", 3),
		ContainerCreateTimeoutMinutes:        int64(getEnvInt("CONTAINER_CREATE_TIMEOUT_MINUTES", 30)),
		MaxQueuedContainerCreatesPerUser:     getEnvInt("MAX_QUEUED_CONTAINER_CREATES_PER_USER", 5),
		ContainerCreateOutboxIntervalSeconds: int64(getEnvInt("CONTAINER_CREATE_OUTBOX_INTERVAL_SECONDS", 1)),
		ContainerCreateOutboxBatchSize:       getEnvInt("CONTAINER_CREATE_OUTBOX_BATCH_SIZE", 10),
		ReportsUsageSnapshotIntervalSeconds:  getEnvInt64("REPORTS_USAGE_SNAPSHOT_INTERVAL_SECONDS", config.DefaultReportsUsageSnapshotIntervalSeconds),
		MaxStagedSourceBytesPerUser:          getEnvInt64("MAX_STAGED_SOURCE_BYTES_PER_USER", 1024*1024*1024),
		MaxQueuedBuildsPerUser:               getEnvInt("MAX_QUEUED_BUILDS_PER_USER", 10),
		ComposeUploadMaxBytes:                getEnvInt64("COMPOSE_UPLOAD_MAX_BYTES", 100<<20),
		ComposePipelineTimeoutMinutes:        int64(getEnvInt("COMPOSE_PIPELINE_TIMEOUT_MINUTES", 30)),
		ComposeDeployWorkerCount:             getEnvInt("COMPOSE_DEPLOY_WORKER_COUNT", 2),
		ComposeOutboxIntervalSeconds:         int64(getEnvInt("COMPOSE_OUTBOX_INTERVAL_SECONDS", 1)),
		ComposeOutboxBatchSize:               getEnvInt("COMPOSE_OUTBOX_BATCH_SIZE", 10),
		ComposeDeployMaxAttempts:             getEnvInt("COMPOSE_DEPLOY_MAX_ATTEMPTS", 3),
		MaxQueuedComposeDeploysPerUser:       getEnvInt("MAX_QUEUED_COMPOSE_DEPLOYS_PER_USER", 5),
		ComposeBuildPollIntervalSeconds:      int64(getEnvInt("COMPOSE_BUILD_POLL_INTERVAL_SECONDS", 3)),
		ComposeDependencyWaitTimeoutMinutes:  int64(getEnvInt("COMPOSE_DEPENDENCY_WAIT_TIMEOUT_MINUTES", 5)),
		ComposeDependencyPollIntervalSeconds: int64(getEnvInt("COMPOSE_DEPENDENCY_POLL_INTERVAL_SECONDS", 2)),
		ComposeCoordinatorIntervalSeconds:    int64(getEnvInt("COMPOSE_COORDINATOR_INTERVAL_SECONDS", 2)),
		HostMinFreeDiskBytes:                 getEnvInt64("HOST_MIN_FREE_DISK_BYTES", 1024*1024*1024),
		GitSourcesEnabled:                    getEnvBool("GIT_SOURCES_ENABLED", true),
		GitAllowedHosts:                      getEnvStringList("GIT_ALLOWED_HOSTS", []string{"github.com", "gitlab.com", "bitbucket.org"}),
		GitCloneTimeoutSeconds:               int64(getEnvInt("GIT_CLONE_TIMEOUT_SECONDS", 60)),
		GitMaxRepositoryBytes:                getEnvInt64("GIT_MAX_REPOSITORY_BYTES", 200*1024*1024),
		TelemetryMaxLogTailLines:             getEnvInt("TELEMETRY_MAX_LOG_TAIL_LINES", 1000),
		TelemetryMaxLogStreamsPerUser:        getEnvInt("TELEMETRY_MAX_LOG_STREAMS_PER_USER", 5),
		TelemetryMaxTerminalSessionsPerUser:  getEnvInt("TELEMETRY_MAX_TERMINAL_SESSIONS_PER_USER", 2),
		TelemetryTerminalIdleTimeoutSeconds:  int64(getEnvInt("TELEMETRY_TERMINAL_IDLE_TIMEOUT_SECONDS", 300)),
		TelemetryTerminalMaxDurationSeconds:  int64(getEnvInt("TELEMETRY_TERMINAL_MAX_DURATION_SECONDS", 3600)),
		TelemetryAllowedExecCommands:         getEnvStringList("TELEMETRY_ALLOWED_EXEC_COMMANDS", []string{"/bin/sh", "/bin/bash", "/busybox/sh"}),
		TelemetryMaxCommandArgs:              getEnvInt("TELEMETRY_MAX_COMMAND_ARGS", 8),
		TelemetryMaxCommandArgBytes:          getEnvInt("TELEMETRY_MAX_COMMAND_ARG_BYTES", 128),
		TelemetryWSReadLimitBytes:            getEnvInt64("TELEMETRY_WS_READ_LIMIT_BYTES", 4096),
	}

	cfgManager, err := config.NewManager("./config/config.json", defaultCfg)
	if err != nil {
		fatal(logger, "failed to initialize config manager", "error", err)
	}

	application, err := app.New(app.Config{
		Port:          port,
		HTTPPort:      httpPort,
		DBURL:         dbURL,
		RabbitMQURL:   rabbitMQURL,
		SSOTarget:     ssoTarget,
		InternalToken: internalToken,
		HostDiskPath:  hostDiskPath,
		ConfigManager: cfgManager,
		ClickHouse: app.ClickHouseConfig{
			Addr:     getEnvString("CLICKHOUSE_ADDR", ""),
			Database: getEnvString("CLICKHOUSE_DB", "dcm_reports"),
			Username: getEnvString("CLICKHOUSE_USER", ""),
			Password: getEnvString("CLICKHOUSE_PASSWORD", ""),
		},
		ObjectStorage: objectstorage.Config{
			Endpoint:  requiredEnv(logger, "OBJECT_STORAGE_ENDPOINT"),
			Bucket:    getEnvString("OBJECT_STORAGE_BUCKET", "dcm-builds"),
			AccessKey: requiredEnv(logger, "OBJECT_STORAGE_ACCESS_KEY"),
			SecretKey: requiredEnv(logger, "OBJECT_STORAGE_SECRET_KEY"),
			UseSSL:    getEnvBool("OBJECT_STORAGE_USE_SSL", false),
		},
	}, logger)
	if err != nil {
		fatal(logger, "failed to initialize core application", "error", err)
	}

	go func() {
		if err := application.Run(); err != nil {
			fatal(logger, "core grpc server failed", "error", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop

	application.Stop()
}

func fatal(logger *slog.Logger, msg string, args ...any) {
	logger.Error(msg, args...)
	os.Exit(1)
}
