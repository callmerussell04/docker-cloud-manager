package main

import (
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/app"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/platform/logging"
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
		BaseDomain:               baseDomain,
		DefaultMemoryReservation: getEnvInt64("DEFAULT_MEMORY_RESERVATION_BYTES", 256*1024*1024), // 256 MB
		ReservedSystemMemory:     getEnvInt64("RESERVED_SYSTEM_MEMORY_BYTES", 2*1024*1024*1024),  // 2 GB
		OvercommitFactor:         getEnvFloat("OVERCOMMIT_FACTOR", 1.5),
		MaxBurstMultiplier:       getEnvInt64("MAX_BURST_MULTIPLIER", 4),
		DefaultCPUShares:         getEnvInt64("DEFAULT_CPU_SHARES", 1024),
		HighLoadCPUShares:        getEnvInt64("HIGH_LOAD_CPU_SHARES", 512),
		HighLoadContainerCount:   getEnvInt("HIGH_LOAD_CONTAINER_COUNT", 5),
		ContainerStopTimeout:     getEnvInt("CONTAINER_STOP_TIMEOUT", 10),
		MaxLogSize:               getEnvString("MAX_LOG_SIZE", "10m"),
		MaxLogFiles:              getEnvString("MAX_LOG_FILES", "3"),
		ContainerDiskQuota:       getEnvString("CONTAINER_DISK_QUOTA", "1G"),
		MaxVolumesPerUser:        getEnvInt("MAX_VOLUMES_PER_USER", 5),
		MaxContainersPerUser:     getEnvInt("MAX_CONTAINERS_PER_USER", 10),
		RegistryAPIURL:           registryAPIURL,
		RegistryPublicURL:        registryPublicURL,
		ContainerTTL:             time.Duration(getEnvInt("CONTAINER_TTL_HOURS", 24)) * time.Hour,
	}

	cfgManager, err := config.NewManager("./config/config.json", defaultCfg)
	if err != nil {
		fatal(logger, "failed to initialize config manager", "error", err)
	}

	application, err := app.New(port, httpPort, dbURL, registryContainerName, builderHTTPUrl, ssoTarget, internalToken, cfgManager, logger)
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
