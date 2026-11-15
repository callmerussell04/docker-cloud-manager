package config_test

import (
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/envtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const secret = "s3cret-value"

func requiredEnv() map[string]string {
	return map[string]string{
		"CORE_GRPC_TARGET":          "core:50052",
		"BUILDER_SERVICE_TOKEN":     secret,
		"RABBITMQ_URL":              "amqp://u:" + secret + "@mq",
		"OBJECT_STORAGE_ENDPOINT":   "minio:9000",
		"OBJECT_STORAGE_ACCESS_KEY": secret,
		"OBJECT_STORAGE_SECRET_KEY": secret,
		"BUILDER_INSTANCE_ID":       "builder-1",
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load(envtest.Lookup(requiredEnv()))

	require.NoError(t, err)
	assert.Equal(t, "/run/dcm-registry/ca.crt", cfg.RegistryCAFile)
	assert.Equal(t, "/tmp/builds", cfg.StoragePath)
	assert.Equal(t, "dcm-builds", cfg.ObjectStorage.Bucket)
	assert.False(t, cfg.ObjectStorage.UseSSL)
	assert.Equal(t, "builder-1", cfg.InstanceID)
	assert.Empty(t, cfg.TenantCgroupParent)
	assert.Equal(t, config.BuilderConfig{
		BuildMemoryBytes:          512 << 20,
		BuildCPUQuota:             100000,
		BuildCPUPeriod:            100000,
		BuildMemorySwapMultiplier: 2,
		BuildPidsLimit:            512,
		LogsDirPath:               "/tmp/build_logs",
		StoragePath:               "/tmp/builds",
		RegistryURL:               "registry:5000",
		BuildNetworkName:          "build_net",
		KanikoImage:               "gcr.io/kaniko-project/executor@sha256:4e7a52dd1f14872430652bb3b027405b8dfd17c4538751c620ac005741ef9698",
		MaxBuildTime:              10 * time.Minute,
		MaxConcurrentBuilds:       2,
		MaxUnpackedSizeBytes:      500 << 20,
		MaxBuildLogSizeBytes:      5 << 20,
		BuildCancelPollInterval:   2 * time.Second,
		BuildMaxDiskBytes:         10 << 30,
	}, cfg.Builder)
}

func TestLoadOverrides(t *testing.T) {
	values := requiredEnv()
	values["BUILDER_INSTANCE_ID"] = "builder-7"
	values["MAX_BUILD_TIME_MINUTES"] = "3"
	values["OBJECT_STORAGE_BUCKET"] = ""
	values["MAX_LOG_SIZE_BYTES"] = "100"
	values["TENANT_CGROUP_PARENT"] = "dcm-tenants.slice"

	cfg, err := config.Load(envtest.Lookup(values))
	require.NoError(t, err)
	assert.Equal(t, "dcm-tenants.slice", cfg.TenantCgroupParent)
	assert.Equal(t, "builder-7", cfg.InstanceID)
	assert.Equal(t, 3*time.Minute, cfg.Builder.MaxBuildTime)
	assert.Equal(t, "", cfg.ObjectStorage.Bucket, "a value set to empty replaces the default")
	assert.Equal(t, int64(100), cfg.Builder.MaxBuildLogSizeBytes, "the legacy log size key is the fallback")
}

func TestLoadPrefersTheBuildLogSizeKey(t *testing.T) {
	values := requiredEnv()
	values["MAX_LOG_SIZE_BYTES"] = "100"
	values["MAX_BUILD_LOG_SIZE_BYTES"] = "200"

	cfg, err := config.Load(envtest.Lookup(values))

	require.NoError(t, err)
	assert.Equal(t, int64(200), cfg.Builder.MaxBuildLogSizeBytes)
}

func TestLoadReportsRequiredSettingsInOrderWithoutValues(t *testing.T) {
	envtest.AssertRequired(t, config.Load, requiredEnv(), []string{
		"CORE_GRPC_TARGET",
		"BUILDER_SERVICE_TOKEN",
		"RABBITMQ_URL",
		"OBJECT_STORAGE_ENDPOINT",
		"OBJECT_STORAGE_ACCESS_KEY",
		"OBJECT_STORAGE_SECRET_KEY",
		"BUILDER_INSTANCE_ID",
	})
}

// Build CPU comes from the variable Core reads, in millicores, so one setting
// configures both; the CPU quota is that share of the CPU period.
func TestLoadReadsBuildCPUInMillicores(t *testing.T) {
	values := requiredEnv()
	values["BUILD_CPU_MILLICORES"] = "2500"
	values["BUILD_CPU_PERIOD"] = "50000"
	values["BUILD_CPU_QUOTA"] = "777"

	cfg, err := config.Load(envtest.Lookup(values))

	require.NoError(t, err)
	assert.Equal(t, int64(125000), cfg.Builder.BuildCPUQuota, "2.5 cores of a 50000 period")
	assert.Equal(t, int64(50000), cfg.Builder.BuildCPUPeriod)
}
