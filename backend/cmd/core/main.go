package main

import (
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/app"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
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
	port := getEnvInt("CORE_GRPC_PORT", 50052)
	httpPort := getEnvInt("CORE_HTTP_PORT", 8083)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is not set")
	}

	baseDomain := os.Getenv("BASE_DOMAIN")
	if baseDomain == "" {
		log.Fatal("BASE_DOMAIN environment variable is not set")
	}

	builderHTTPUrl := os.Getenv("BUILDER_HTTP_TARGET")
	if builderHTTPUrl == "" {
		log.Fatal("BUILDER_HTTP_TARGET environment variable is not set")
	}

	ssoTarget := os.Getenv("SSO_GRPC_TARGET")
	if ssoTarget == "" {
		log.Fatal("SSO_GRPC_TARGET environment variable is not set")
	}

	internalToken := os.Getenv("INTERNAL_SERVICE_TOKEN")
	if internalToken == "" {
		log.Fatal("INTERNAL_SERVICE_TOKEN environment variable is not set")
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
		log.Fatalf("failed to initialize config manager: %v", err)
	}

	application, err := app.New(port, httpPort, dbURL, registryContainerName, builderHTTPUrl, ssoTarget, internalToken, cfgManager)
	if err != nil {
		log.Fatalf("failed to initialize core application: %v", err)
	}

	go func() {
		if err := application.Run(); err != nil {
			log.Fatalf("grpc server failed: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop

	application.Stop()
}
