package dto

type VolumeMountDTO struct {
	VolumeID   string `json:"volume_id" binding:"required,uuid"`
	MountPath  string `json:"mount_path" binding:"required"`
	IsReadOnly bool   `json:"is_readonly"`
}

type CreateContainerDTO struct {
	Name         string            `json:"name" binding:"required"`
	ImageTag     string            `json:"image_tag" binding:"required"`
	InternalPort int               `json:"internal_port"`
	EnvVars      map[string]string `json:"env_vars"`
	VolumeMounts []VolumeMountDTO  `json:"volume_mounts"`
	DomainPrefix string            `json:"domain_prefix" binding:"omitempty,max=30"`
}

type ExposeContainerDTO struct {
	DomainPrefix string `json:"domain_prefix" binding:"required,max=30"`
	InternalPort int    `json:"internal_port" binding:"required,min=1"`
}

type CreateVolumeDTO struct {
	Name       string            `json:"name" binding:"required"`
	Driver     string            `json:"driver"`
	DriverOpts map[string]string `json:"driver_opts"`
}

type RegisterImageDTO struct {
	Tag    string `json:"tag" binding:"required"`
	SizeMB int    `json:"size_mb" binding:"required,min=1"`
}
