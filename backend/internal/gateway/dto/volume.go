package dto

type VolumeDTO struct {
	ID            string `json:"id"`
	DockerName    string `json:"docker_name"`
	Driver        string `json:"driver"`
	Status        string `json:"status"`
	LastError     string `json:"last_error"`
	CreatedAt     int64  `json:"created_at"`
	OwnerID       string `json:"owner_id"`
	OwnerUsername string `json:"owner_username"`
}

type CreateVolumeDTO struct {
	Name string `json:"name" binding:"required"`
}

type VolumeMountDTO struct {
	VolumeID   string `json:"volume_id" binding:"required,uuid"`
	MountPath  string `json:"mount_path" binding:"required"`
	IsReadOnly bool   `json:"is_readonly"`
}

type PaginatedVolumes struct {
	Volumes    []VolumeDTO `json:"volumes"`
	TotalCount int32       `json:"total_count"`
}
