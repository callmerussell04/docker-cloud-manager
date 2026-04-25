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
