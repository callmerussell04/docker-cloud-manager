package model

type SystemConfig struct {
	BaseDomain                    string
	DefaultMemoryReservationBytes int64
	ReservedSystemMemoryBytes     int64
	OvercommitFactor              float64
	MaxBurstMultiplier            int64
	DefaultCpuShares              int64
	HighLoadCpuShares             int64
	HighLoadContainerCount        int
	ContainerStopTimeout          int
	MaxLogSize                    string
	MaxLogFiles                   string
	ContainerDiskQuota            string
	MaxVolumesPerUser             int
	MaxContainersPerUser          int
	RegistryApiUrl                string
	RegistryPublicUrl             string
	ContainerTtlHours             int64
}
