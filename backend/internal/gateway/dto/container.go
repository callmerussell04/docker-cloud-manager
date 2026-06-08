package dto

type ContainerDTO struct {
	ID            string `json:"id"`
	DockerID      string `json:"docker_id"`
	Name          string `json:"name"`
	ImageTag      string `json:"image_tag"`
	InternalPort  int32  `json:"internal_port"`
	DomainPrefix  string `json:"domain_prefix"`
	Status        string `json:"status"`
	DesiredStatus string `json:"desired_status"`
	LastError     string `json:"last_error"`
	LastExitCode  *int   `json:"last_exit_code,omitempty"`
	TTLDeadline   int64  `json:"ttl_deadline"`
	CreatedAt     int64  `json:"created_at"`
	OwnerID       string `json:"owner_id"`
	OwnerUsername string `json:"owner_username"`
}

type UserContainerDTO struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ImageTag      string `json:"image_tag"`
	InternalPort  int32  `json:"internal_port"`
	DomainPrefix  string `json:"domain_prefix"`
	Status        string `json:"status"`
	DesiredStatus string `json:"desired_status"`
	LastError     string `json:"last_error"`
	LastExitCode  *int   `json:"last_exit_code,omitempty"`
	TTLDeadline   int64  `json:"ttl_deadline"`
	CreatedAt     int64  `json:"created_at"`
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

type ContainerStatsDTO struct {
	CPUPercentage    float64 `json:"cpu_percentage"`
	MemoryUsageBytes int64   `json:"memory_usage_bytes"`
	MemoryLimitBytes int64   `json:"memory_limit_bytes"`
	NetworkRxBytes   int64   `json:"network_rx_bytes"`
	NetworkTxBytes   int64   `json:"network_tx_bytes"`
}

type PaginatedContainers struct {
	Containers []ContainerDTO `json:"containers"`
	TotalCount int32          `json:"total_count"`
}
