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

func getEnvString(key string, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
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

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fatal(logger, "required environment variable is not set", "env_var", "DATABASE_URL")
	}

	baseDomain := os.Getenv("BASE_DOMAIN")
	if baseDomain == "" {
		fatal(logger, "required environment variable is not set", "env_var", "BASE_DOMAIN")
	}

	builderHTTPUrl := os.Getenv("BUILDER_HTTP_TARGET")
	if builderHTTPUrl == "" {
		fatal(logger, "required environment variable is not set", "env_var", "BUILDER_HTTP_TARGET")
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
		TTLWorkerIntervalSeconds:             int64(getEnvInt("TTL_WORKER_INTERVAL_SECONDS", 60)),
		GCWorkerIntervalMinutes:              int64(getEnvInt("GC_WORKER_INTERVAL_MINUTES", 60)),
		StaleBuildTimeoutMinutes:             int64(getEnvInt("STALE_BUILD_TIMEOUT_MINUTES", 30)),
		EventSyncIntervalSeconds:             int64(getEnvInt("EVENT_SYNC_INTERVAL_SECONDS", 30)),
		EventReconnectDelaySeconds:           int64(getEnvInt("EVENT_RECONNECT_DELAY_SECONDS", 5)),
		BuildOutboxIntervalSeconds:           int64(getEnvInt("BUILD_OUTBOX_INTERVAL_SECONDS", 1)),
		BuildOutboxBatchSize:                 getEnvInt("BUILD_OUTBOX_BATCH_SIZE", 10),
		ComposeUploadMaxBytes:                getEnvInt64("COMPOSE_UPLOAD_MAX_BYTES", 100<<20),
		ComposePipelineTimeoutMinutes:        int64(getEnvInt("COMPOSE_PIPELINE_TIMEOUT_MINUTES", 30)),
		ComposeBuilderHTTPTimeoutSeconds:     int64(getEnvInt("COMPOSE_BUILDER_HTTP_TIMEOUT_SECONDS", 30)),
		ComposeBuildPollIntervalSeconds:      int64(getEnvInt("COMPOSE_BUILD_POLL_INTERVAL_SECONDS", 3)),
		ComposeDependencyWaitTimeoutMinutes:  int64(getEnvInt("COMPOSE_DEPENDENCY_WAIT_TIMEOUT_MINUTES", 5)),
		ComposeDependencyPollIntervalSeconds: int64(getEnvInt("COMPOSE_DEPENDENCY_POLL_INTERVAL_SECONDS", 2)),
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
		Port:           port,
		HTTPPort:       httpPort,
		DBURL:          dbURL,
		BuilderHTTPURL: builderHTTPUrl,
		RabbitMQURL:    rabbitMQURL,
		SSOTarget:      ssoTarget,
		InternalToken:  internalToken,
		ConfigManager:  cfgManager,
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
