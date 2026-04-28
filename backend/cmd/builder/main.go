package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/app"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/objectstorage"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
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

func getEnvBool(key string, fallback bool) bool {
	if value, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return fallback
}

func main() {
	logger := logging.NewLogger("builder", logging.ConfigFromEnv())
	slog.SetDefault(logger)

	port := getEnvInt("BUILDER_PORT", 8082)

	coreTarget := os.Getenv("CORE_GRPC_TARGET")
	if coreTarget == "" {
		fatal(logger, "required environment variable is not set", "env_var", "CORE_GRPC_TARGET")
	}

	internalToken := os.Getenv("INTERNAL_SERVICE_TOKEN")
	if internalToken == "" {
		fatal(logger, "required environment variable is not set", "env_var", "INTERNAL_SERVICE_TOKEN")
	}

	rabbitMQURL := os.Getenv("RABBITMQ_URL")
	if rabbitMQURL == "" {
		fatal(logger, "required environment variable is not set", "env_var", "RABBITMQ_URL")
	}

	objectStorageEndpoint := os.Getenv("OBJECT_STORAGE_ENDPOINT")
	if objectStorageEndpoint == "" {
		fatal(logger, "required environment variable is not set", "env_var", "OBJECT_STORAGE_ENDPOINT")
	}
	objectStorageAccessKey := os.Getenv("OBJECT_STORAGE_ACCESS_KEY")
	if objectStorageAccessKey == "" {
		fatal(logger, "required environment variable is not set", "env_var", "OBJECT_STORAGE_ACCESS_KEY")
	}
	objectStorageSecretKey := os.Getenv("OBJECT_STORAGE_SECRET_KEY")
	if objectStorageSecretKey == "" {
		fatal(logger, "required environment variable is not set", "env_var", "OBJECT_STORAGE_SECRET_KEY")
	}
	objectStorageBucket := getEnvString("OBJECT_STORAGE_BUCKET", "dcm-builds")

	storagePath := getEnvString("BUILD_STORAGE_PATH", "/tmp/builds")
	logsDirPath := getEnvString("BUILD_LOGS_PATH", "/tmp/build_logs")
	registryURL := getEnvString("REGISTRY_URL", "registry:5000")
	buildNetworkName := getEnvString("BUILD_NETWORK_NAME", "build_net")

	builderConfig := config.BuilderConfig{
		BuildMemoryBytes:          getEnvInt64("BUILD_MEMORY_BYTES", 512*1024*1024),
		BuildCPUQuota:             getEnvInt64("BUILD_CPU_QUOTA", 100000),
		BuildCPUPeriod:            getEnvInt64("BUILD_CPU_PERIOD", 100000),
		BuildMemorySwapMultiplier: getEnvFloat("BUILD_MEMORY_SWAP_MULTIPLIER", 2),
		BuildPidsLimit:            getEnvInt64("BUILD_PIDS_LIMIT", 512),
		LogsDirPath:               logsDirPath,
		StoragePath:               storagePath,
		RegistryURL:               registryURL,
		BuildNetworkName:          buildNetworkName,
		KanikoImage:               getEnvString("KANIKO_IMAGE", "gcr.io/kaniko-project/executor:latest"),
		MaxBuildTime:              time.Duration(getEnvInt("MAX_BUILD_TIME_MINUTES", 10)) * time.Minute,
		MaxConcurrentBuilds:       getEnvInt("MAX_CONCURRENT_BUILDS", 2),
		MaxUploadSizeBytes:        getEnvInt64("MAX_UPLOAD_SIZE_BYTES", 50<<20),
		MaxArchiveSizeBytes:       getEnvInt64("MAX_ARCHIVE_SIZE_BYTES", 50<<20),
		MaxUnpackedSizeBytes:      getEnvInt64("MAX_UNPACKED_SIZE_BYTES", 500*1024*1024),
		MaxBuildLogSizeBytes:      getEnvInt64("MAX_BUILD_LOG_SIZE_BYTES", getEnvInt64("MAX_LOG_SIZE_BYTES", 5*1024*1024)),
	}

	application, err := app.New(app.Config{
		Port:          port,
		CoreTarget:    coreTarget,
		InternalToken: internalToken,
		Builder:       builderConfig,
		StoragePath:   storagePath,
		RabbitMQURL:   rabbitMQURL,
		ObjectStorage: objectstorage.Config{
			Endpoint:  objectStorageEndpoint,
			Bucket:    objectStorageBucket,
			AccessKey: objectStorageAccessKey,
			SecretKey: objectStorageSecretKey,
			UseSSL:    getEnvBool("OBJECT_STORAGE_USE_SSL", false),
		},
	}, logger)
	if err != nil {
		fatal(logger, "failed to initialize builder app", "error", err)
	}

	go func() {
		if err := application.Run(); err != nil {
			fatal(logger, "builder server failed", "error", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	application.Stop(shutdownCtx)
}

func fatal(logger *slog.Logger, msg string, args ...any) {
	logger.Error(msg, args...)
	os.Exit(1)
}
