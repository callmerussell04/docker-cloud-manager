package config

import "time"

type SystemConfig struct {
	BaseDomain               string        `json:"base_domain"`
	DefaultMemoryReservation int64         `json:"default_memory_reservation_bytes"`
	ReservedSystemMemory     int64         `json:"reserved_system_memory_bytes"`
	OvercommitFactor         float64       `json:"overcommit_factor"`
	MaxBurstMultiplier       int64         `json:"max_burst_multiplier"`
	DefaultCPUShares         int64         `json:"default_cpu_shares"`
	HighLoadCPUShares        int64         `json:"high_load_cpu_shares"`
	HighLoadContainerCount   int           `json:"high_load_container_count"`
	ContainerStopTimeout     int           `json:"container_stop_timeout"`
	MaxLogSize               string        `json:"max_log_size"`
	MaxLogFiles              string        `json:"max_log_files"`
	ContainerDiskQuota       string        `json:"container_disk_quota"`
	MaxVolumesPerUser        int           `json:"max_volumes_per_user"`
	MaxContainersPerUser     int           `json:"max_containers_per_user"`
	RegistryAPIURL           string        `json:"registry_api_url"`
	RegistryPublicURL        string        `json:"registry_public_url"`
	ContainerTTL             time.Duration `json:"container_ttl"`
}
