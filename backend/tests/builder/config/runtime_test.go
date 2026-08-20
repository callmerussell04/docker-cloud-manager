package config_test

import (
	"context"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/config"
	builderconfigmocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/builder/config"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestRuntimeManagerGetAndRefreshWithoutSourceReturnsDefaults(t *testing.T) {
	defaults := defaultBuilderConfig()
	manager := config.NewRuntimeManager(defaults, nil, nil)

	require.Equal(t, defaults, manager.Get())
	require.Equal(t, defaults, manager.Refresh(context.Background()))
}

func TestRuntimeManagerRefreshUpdatesRuntimeFieldsAndPreservesLocalFields(t *testing.T) {
	defaults := defaultBuilderConfig()
	source := builderconfigmocks.NewRuntimeSource(t)
	manager := config.NewRuntimeManager(defaults, source, nil)
	remote := config.BuilderConfig{
		LogsDirPath:               "/remote/logs",
		StoragePath:               "/remote/storage",
		RegistryURL:               "remote-registry",
		BuildMemoryBytes:          2048,
		BuildCPUQuota:             200000,
		BuildCPUPeriod:            200000,
		BuildMemorySwapMultiplier: 3,
		BuildPidsLimit:            1024,
		BuildNetworkName:          "remote_net",
		KanikoImage:               "kaniko:remote",
		MaxBuildTime:              7 * time.Minute,
		MaxConcurrentBuilds:       4,
		MaxUnpackedSizeBytes:      700,
		MaxBuildLogSizeBytes:      800,
	}

	source.EXPECT().GetBuilderConfig(mock.Anything).Return(remote, nil)
	got := manager.Refresh(context.Background())

	require.Equal(t, defaults.LogsDirPath, got.LogsDirPath)
	require.Equal(t, defaults.StoragePath, got.StoragePath)
	require.Equal(t, defaults.RegistryURL, got.RegistryURL)
	require.Equal(t, defaults.BuildCancelPollInterval, got.BuildCancelPollInterval)
	require.Equal(t, int64(2048), got.BuildMemoryBytes)
	require.Equal(t, int64(200000), got.BuildCPUQuota)
	require.Equal(t, "remote_net", got.BuildNetworkName)
	require.Equal(t, "kaniko:remote", got.KanikoImage)
	require.Equal(t, 7*time.Minute, got.MaxBuildTime)
	require.Equal(t, 4, got.MaxConcurrentBuilds)
	require.Equal(t, got, manager.Get())
}

func TestRuntimeManagerRefreshKeepsPreviousConfigOnSourceError(t *testing.T) {
	defaults := defaultBuilderConfig()
	source := builderconfigmocks.NewRuntimeSource(t)
	manager := config.NewRuntimeManager(defaults, source, nil)
	updated := defaults
	updated.BuildMemoryBytes = 4096

	source.EXPECT().GetBuilderConfig(mock.Anything).Return(updated, nil).Once()
	require.Equal(t, int64(4096), manager.Refresh(context.Background()).BuildMemoryBytes)
	source.EXPECT().GetBuilderConfig(mock.Anything).Return(config.BuilderConfig{}, context.DeadlineExceeded).Once()

	got := manager.Refresh(context.Background())
	require.Equal(t, int64(4096), got.BuildMemoryBytes)
	require.Equal(t, updated, manager.Get())
}

func defaultBuilderConfig() config.BuilderConfig {
	return config.BuilderConfig{
		LogsDirPath:               "/local/logs",
		StoragePath:               "/local/storage",
		RegistryURL:               "local-registry",
		BuildMemoryBytes:          512,
		BuildCPUQuota:             100000,
		BuildCPUPeriod:            100000,
		BuildMemorySwapMultiplier: 2,
		BuildPidsLimit:            512,
		BuildNetworkName:          "build_net",
		KanikoImage:               "kaniko:test",
		MaxBuildTime:              time.Minute,
		MaxConcurrentBuilds:       1,
		MaxUnpackedSizeBytes:      500,
		MaxBuildLogSizeBytes:      600,
		BuildCancelPollInterval:   2 * time.Second,
	}
}
