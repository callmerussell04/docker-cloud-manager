package dto

type UserStatsDTO struct {
	ContainersTotal    int32 `json:"containers_total"`
	ContainersRunning  int32 `json:"containers_running"`
	ContainersQuota    int32 `json:"containers_quota"`
	RamUsedBytes       int64 `json:"ram_used_bytes"`
	RamQuotaBytes      int64 `json:"ram_quota_bytes"`
	CPUUsedMillicores  int64 `json:"cpu_used_millicores"`
	CPUQuotaMillicores int64 `json:"cpu_quota_millicores"`
	DiskUsedMB         int32 `json:"disk_used_mb"`
	DiskQuotaMB        int32 `json:"disk_quota_mb"`
	VolumesTotal       int32 `json:"volumes_total"`
	VolumesQuota       int32 `json:"volumes_quota"`
	ImagesTotal        int32 `json:"images_total"`
	ProjectsTotal      int32 `json:"projects_total"`
}

type SystemMonitoringDTO struct {
	CPUPercent                    float64  `json:"cpu_percent"`
	MemoryTotalBytes              int64    `json:"memory_total_bytes"`
	MemoryUsedBytes               int64    `json:"memory_used_bytes"`
	MemoryAvailableBytes          int64    `json:"memory_available_bytes"`
	DiskTotalBytes                int64    `json:"disk_total_bytes"`
	DiskUsedBytes                 int64    `json:"disk_used_bytes"`
	DiskFreeBytes                 int64    `json:"disk_free_bytes"`
	DCMReservedMemoryBytes        int64    `json:"dcm_reserved_memory_bytes"`
	DCMReservedBuildMemoryBytes   int64    `json:"dcm_reserved_build_memory_bytes"`
	DCMReservedCPUMillicores      int64    `json:"dcm_reserved_cpu_millicores"`
	DCMReservedBuildCPUMillicores int64    `json:"dcm_reserved_build_cpu_millicores"`
	DCMBorrowedMemoryBytes        int64    `json:"dcm_borrowed_memory_bytes"`
	RootfsQuotaStatus             string   `json:"rootfs_quota_status"`
	DCMDiskUsedBytes              int64    `json:"dcm_disk_used_bytes"`
	DCMReservedVolumeBytes        int64    `json:"dcm_reserved_volume_bytes"`
	DCMUnusedVolumePromiseBytes   int64    `json:"dcm_unused_volume_promise_bytes"`
	VolumeSizeMode                string   `json:"volume_size_mode"`
	HostMinFreeDiskBytes          int64    `json:"host_min_free_disk_bytes"`
	BuildClaimCap                 int      `json:"build_claim_cap"`
	MaxConcurrentBuilds           int      `json:"max_concurrent_builds"`
	BuildsActive                  int      `json:"builds_active"`
	CPUTotalMillicores            int64    `json:"cpu_total_millicores"`
	AdmissionStatus               string   `json:"admission_status"`
	AdmissionReasons              []string `json:"admission_reasons"`
	ContainersTotal               int32    `json:"containers_total"`
	ContainersRunning             int32    `json:"containers_running"`
	ContainersStopped             int32    `json:"containers_stopped"`
	ContainersError               int32    `json:"containers_error"`
	ContainersMissing             int32    `json:"containers_missing"`
	VolumesTotal                  int32    `json:"volumes_total"`
	ImagesTotal                   int32    `json:"images_total"`
	BuildsTotal                   int32    `json:"builds_total"`
	ProjectsTotal                 int32    `json:"projects_total"`
	ObservedAt                    int64    `json:"observed_at"`
}
