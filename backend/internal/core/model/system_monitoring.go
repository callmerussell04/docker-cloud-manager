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
	CPUPercent             float64
	MemoryTotalBytes       int64
	MemoryUsedBytes        int64
	MemoryAvailableBytes   int64
	DiskTotalBytes         int64
	DiskUsedBytes          int64
	DiskFreeBytes          int64
	DCMReservedMemoryBytes int64
	DCMDiskUsedBytes       int64
	ContainersTotal        int
	ContainersRunning      int
	ContainersStopped      int
	ContainersError        int
	ContainersMissing      int
	VolumesTotal           int
	ImagesTotal            int
	BuildsTotal            int
	ProjectsTotal          int
	ObservedAt             time.Time
}
