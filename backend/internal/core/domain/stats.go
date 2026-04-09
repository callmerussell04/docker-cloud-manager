package domain

type UserStats struct {
	ContainersTotal   int
	ContainersRunning int
	ContainersQuota   int
	RamUsedBytes      int64
	RamQuotaBytes     int64
	DiskUsedMB        int
	DiskQuotaMB       int
	VolumesTotal      int
	VolumesQuota      int
	ImagesTotal       int
	ProjectsTotal     int
}
