package config

import (
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
)

// Bootstrap is Builder's process-lifetime configuration. Its Builder field seeds
// the RuntimeManager, which refreshes runtime fields from Core.
type Bootstrap struct {
	RegistryCAFile string
	CoreTarget     string
	InternalToken  string
	Builder        BuilderConfig
	StoragePath    string
	RabbitMQURL    string
	// InstanceID is empty when unset; app.New falls back to the hostname.
	InstanceID    string
	ObjectStorage objectstorage.Config
}

// Load reads Builder's settings. Its error names the first missing required setting.
func Load(lookup envconfig.Lookup) (Bootstrap, error) {
	required := envconfig.NewRequired(lookup)
	storagePath := envconfig.String(lookup, "BUILD_STORAGE_PATH", "/tmp/builds")

	cfg := Bootstrap{
		CoreTarget:    required.Get("CORE_GRPC_TARGET"),
		InternalToken: required.Get("INTERNAL_SERVICE_TOKEN"),
		RabbitMQURL:   required.Get("RABBITMQ_URL"),
		ObjectStorage: objectstorage.Config{
			Endpoint:  required.Get("OBJECT_STORAGE_ENDPOINT"),
			AccessKey: required.Get("OBJECT_STORAGE_ACCESS_KEY"),
			SecretKey: required.Get("OBJECT_STORAGE_SECRET_KEY"),
			Bucket:    envconfig.String(lookup, "OBJECT_STORAGE_BUCKET", "dcm-builds"),
			UseSSL:    envconfig.Bool(lookup, "OBJECT_STORAGE_USE_SSL", false),
		},
		RegistryCAFile: envconfig.String(lookup, "REGISTRY_CA_FILE", "/run/dcm-registry/ca.crt"),
		StoragePath:    storagePath,
		InstanceID:     envconfig.String(lookup, "BUILDER_INSTANCE_ID", ""),
		Builder: BuilderConfig{
			BuildMemoryBytes:          envconfig.Int64(lookup, "BUILD_MEMORY_BYTES", 512*1024*1024),
			BuildCPUQuota:             envconfig.Int64(lookup, "BUILD_CPU_QUOTA", 100000),
			BuildCPUPeriod:            envconfig.Int64(lookup, "BUILD_CPU_PERIOD", 100000),
			BuildMemorySwapMultiplier: envconfig.Float64(lookup, "BUILD_MEMORY_SWAP_MULTIPLIER", 2),
			BuildPidsLimit:            envconfig.Int64(lookup, "BUILD_PIDS_LIMIT", 512),
			LogsDirPath:               envconfig.String(lookup, "BUILD_LOGS_PATH", "/tmp/build_logs"),
			StoragePath:               storagePath,
			RegistryURL:               envconfig.String(lookup, "REGISTRY_URL", "registry:5000"),
			BuildNetworkName:          envconfig.String(lookup, "BUILD_NETWORK_NAME", "build_net"),
			KanikoImage:               envconfig.String(lookup, "KANIKO_IMAGE", "gcr.io/kaniko-project/executor@sha256:4e7a52dd1f14872430652bb3b027405b8dfd17c4538751c620ac005741ef9698"),
			MaxBuildTime:              time.Duration(envconfig.Int(lookup, "MAX_BUILD_TIME_MINUTES", 10)) * time.Minute,
			MaxConcurrentBuilds:       envconfig.Int(lookup, "MAX_CONCURRENT_BUILDS", 2),
			MaxUnpackedSizeBytes:      envconfig.Int64(lookup, "MAX_UNPACKED_SIZE_BYTES", 500*1024*1024),
			MaxBuildLogSizeBytes:      envconfig.Int64(lookup, "MAX_BUILD_LOG_SIZE_BYTES", envconfig.Int64(lookup, "MAX_LOG_SIZE_BYTES", 5*1024*1024)),
			BuildCancelPollInterval:   time.Duration(envconfig.Int(lookup, "BUILD_CANCEL_POLL_INTERVAL_SECONDS", 2)) * time.Second,
		},
	}
	if err := required.Err(); err != nil {
		return Bootstrap{}, err
	}
	return cfg, nil
}
