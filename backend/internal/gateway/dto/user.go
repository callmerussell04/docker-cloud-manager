package dto

type UserStatsDTO struct {
	ContainersTotal   int32 `json:"containers_total"`
	ContainersRunning int32 `json:"containers_running"`
	ContainersQuota   int32 `json:"containers_quota"`
	RamUsedBytes      int64 `json:"ram_used_bytes"`
	RamQuotaBytes     int64 `json:"ram_quota_bytes"`
	DiskUsedMB        int32 `json:"disk_used_mb"`
	DiskQuotaMB       int32 `json:"disk_quota_mb"`
	VolumesTotal      int32 `json:"volumes_total"`
	VolumesQuota      int32 `json:"volumes_quota"`
	ImagesTotal       int32 `json:"images_total"`
	ProjectsTotal     int32 `json:"projects_total"`
}
