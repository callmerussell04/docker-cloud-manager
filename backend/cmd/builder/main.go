package main

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/app"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/config"
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

func getEnvString(key string, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func main() {
	port := getEnvInt("BUILDER_PORT", 8082)

	coreTarget := os.Getenv("CORE_GRPC_TARGET")
	if coreTarget == "" {
		log.Fatal("CORE_GRPC_TARGET environment variable is not set")
	}

	internalToken := os.Getenv("INTERNAL_SERVICE_TOKEN")
	if internalToken == "" {
		log.Fatal("INTERNAL_SERVICE_TOKEN environment variable is not set")
	}

	storagePath := getEnvString("BUILD_STORAGE_PATH", "/tmp/builds")
	logsDirPath := getEnvString("BUILD_LOGS_PATH", "/tmp/build_logs")
	registryURL := getEnvString("REGISTRY_URL", "registry:5000")
	buildNetworkName := getEnvString("BUILD_NETWORK_NAME", "build_net")
	maxUnpackedSize := getEnvInt64("MAX_UNPACKED_SIZE_BYTES", 500*1024*1024)
	maxLogSize := getEnvInt64("MAX_LOG_SIZE_BYTES", 5*1024*1024)

	builderConfig := config.BuilderConfig{
		BuildMemoryBytes:    getEnvInt64("BUILD_MEMORY_BYTES", 512*1024*1024),
		BuildCPUQuota:       getEnvInt64("BUILD_CPU_QUOTA", 100000),
		LogsDirPath:         logsDirPath,
		StoragePath:         storagePath,
		RegistryURL:         registryURL,
		BuildNetworkName:    buildNetworkName,
		MaxBuildTime:        time.Duration(getEnvInt("MAX_BUILD_TIME_MINUTES", 10)) * time.Minute,
		MaxConcurrentBuilds: getEnvInt("MAX_CONCURRENT_BUILDS", 2),
	}

	application, err := app.New(port, coreTarget, internalToken, builderConfig, maxUnpackedSize, maxLogSize, storagePath)
	if err != nil {
		log.Fatalf("failed to initialize builder app: %v", err)
	}

	log.Printf("Builder service starting on port %d", port)
	if err := application.Run(); err != nil {
		log.Fatalf("builder server failed: %v", err)
	}
}
