package dto

type SystemConfigDTO struct {
	BaseDomain                    string  `json:"base_domain"`
	DefaultMemoryReservationBytes int64   `json:"default_memory_reservation_bytes"`
	ReservedSystemMemoryBytes     int64   `json:"reserved_system_memory_bytes"`
	OvercommitFactor              float64 `json:"overcommit_factor"`
	MaxBurstMultiplier            int64   `json:"max_burst_multiplier"`
	DefaultCpuShares              int64   `json:"default_cpu_shares"`
	HighLoadCpuShares             int64   `json:"high_load_cpu_shares"`
	HighLoadContainerCount        int     `json:"high_load_container_count"`
	ContainerStopTimeout          int     `json:"container_stop_timeout"`
	MaxLogSize                    string  `json:"max_log_size"`
	MaxLogFiles                   string  `json:"max_log_files"`
	ContainerDiskQuota            string  `json:"container_disk_quota"`
	MaxVolumesPerUser             int     `json:"max_volumes_per_user"`
	MaxContainersPerUser          int     `json:"max_containers_per_user"`
	RegistryUrl                   string  `json:"registry_url"`
	ContainerTtlHours             int64   `json:"container_ttl_hours"`
}

type PaginatedContainers struct {
	Containers []interface{} `json:"containers"`
	TotalCount int           `json:"total_count"`
}

type PaginatedVolumes struct {
	Volumes    []interface{} `json:"volumes"`
	TotalCount int           `json:"total_count"`
}

type PaginatedImages struct {
	Images     []interface{} `json:"images"`
	TotalCount int           `json:"total_count"`
}

type PaginatedBuilds struct {
	Builds     []interface{} `json:"builds"`
	TotalCount int           `json:"total_count"`
}

type PaginatedProjects struct {
	Projects   []interface{} `json:"projects"`
	TotalCount int           `json:"total_count"`
}
