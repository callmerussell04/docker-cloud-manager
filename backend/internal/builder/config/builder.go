package config

import "time"

type BuilderConfig struct {
	BuildMemoryBytes    int64
	BuildCPUQuota       int64
	LogsDirPath         string
	StoragePath         string
	RegistryURL         string
	BuildNetworkName    string
	MaxBuildTime        time.Duration
	MaxConcurrentBuilds int
}
