package config

import "time"

type ContainerConfig struct {
	BaseDomain               string
	DefaultMemoryReservation int64
	ReservedSystemMemory     int64
	OvercommitFactor         float64
	MaxBurstMultiplier       int64
	DefaultCPUShares         int64
	HighLoadCPUShares        int64
	HighLoadContainerCount   int
	ContainerStopTimeout     int
	MaxLogSize               string
	MaxLogFiles              string
	ContainerDiskQuota       string
	MaxVolumesPerUser        int
	MaxContainersPerUser     int
	RegistryURL              string
	ContainerTTL             time.Duration
}
