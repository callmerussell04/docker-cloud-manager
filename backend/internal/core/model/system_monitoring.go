package model

import "time"

type HostMemoryStats struct {
	TotalBytes     int64
	UsedBytes      int64
	AvailableBytes int64
}

type HostDiskStats struct {
	TotalBytes int64
	UsedBytes  int64
	FreeBytes  int64
}

type ContainerStatusCounts struct {
	Total   int
	Running int
	Stopped int
	Error   int
	Missing int
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
	// DCMBorrowedMemoryBytes is the memory running workloads borrow above
	// their standard limits.
	DCMBorrowedMemoryBytes int64
	DCMDiskUsedBytes       int64
	// DCMReservedVolumeBytes is the sum of the sizes of all volumes not
	// being deleted: the disk promised to volumes.
	DCMReservedVolumeBytes int64
	// DCMUnusedVolumePromiseBytes is, over the same volumes, each size less
	// its measured usage, never below zero: the disk promised to volumes and
	// not used yet, which volume creates count against the disk floor.
	DCMUnusedVolumePromiseBytes int64
	HostMinFreeDiskBytes        int64
	AdmissionStatus             string
	AdmissionReasons            []string
	RootfsQuotaStatus           RootfsQuotaStatus
	VolumeSizeMode              VolumeSizeMode
	ContainersTotal             int
	ContainersRunning           int
	ContainersStopped           int
	ContainersError             int
	ContainersMissing           int
	VolumesTotal                int
	ImagesTotal                 int
	BuildsTotal                 int
	ProjectsTotal               int
	ObservedAt                  time.Time
}

// RootfsQuotaStatus says whether Docker enforces the container root
// filesystem cap (the size storage option) on this host.
type RootfsQuotaStatus string

const (
	RootfsQuotaEnforced    RootfsQuotaStatus = "enforced"
	RootfsQuotaNotEnforced RootfsQuotaStatus = "not_enforced"
	RootfsQuotaUnknown     RootfsQuotaStatus = "unknown"
)

// VolumeSizeMode says how volume sizes are enforced for new volumes.
type VolumeSizeMode string

const (
	// VolumeSizeMeasured: enforcement is off; the volume usage worker stops
	// writers of a volume measured above its size.
	VolumeSizeMeasured VolumeSizeMode = "measured"
	// VolumeSizeEnforced: enforcement is on and the host supports it; new
	// volumes get a kernel-enforced size.
	VolumeSizeEnforced VolumeSizeMode = "enforced"
	// VolumeSizeUnavailable: enforcement is on but the host check failed at
	// startup; new volumes are created measured.
	VolumeSizeUnavailable VolumeSizeMode = "unavailable"
)
