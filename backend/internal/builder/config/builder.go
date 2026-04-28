package config

import "time"

type BuilderConfig struct {
	BuildMemoryBytes          int64
	BuildCPUQuota             int64
	BuildCPUPeriod            int64
	BuildMemorySwapMultiplier float64
	BuildPidsLimit            int64
	LogsDirPath               string
	StoragePath               string
	RegistryURL               string
	BuildNetworkName          string
	KanikoImage               string
	MaxBuildTime              time.Duration
	MaxConcurrentBuilds       int
	MaxUploadSizeBytes        int64
	MaxArchiveSizeBytes       int64
	MaxUnpackedSizeBytes      int64
	MaxBuildLogSizeBytes      int64
}
