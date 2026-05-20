package model

type UserStats struct {
	ContainersTotal   int32
	ContainersRunning int32
	ContainersQuota   int32
	RamUsedBytes      int64
	RamQuotaBytes     int64
	DiskUsedMB        int32
	DiskQuotaMB       int32
	VolumesTotal      int32
	VolumesQuota      int32
	ImagesTotal       int32
	ProjectsTotal     int32
}

type SystemMonitoring struct {
	CPUPercent                  float64
	MemoryTotalBytes            int64
	MemoryUsedBytes             int64
	MemoryAvailableBytes        int64
	DiskTotalBytes              int64
	DiskUsedBytes               int64
	DiskFreeBytes               int64
	DCMReservedMemoryBytes      int64
	DCMReservedBuildMemoryBytes int64
	DCMDiskUsedBytes            int64
	HostMinFreeDiskBytes        int64
	AdmissionStatus             string
	AdmissionReasons            []string
	ContainersTotal             int32
	ContainersRunning           int32
	ContainersStopped           int32
	ContainersError             int32
	ContainersMissing           int32
	VolumesTotal                int32
	ImagesTotal                 int32
	BuildsTotal                 int32
	ProjectsTotal               int32
	ObservedAt                  int64
}
