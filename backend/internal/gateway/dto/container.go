package dto

// ContainerRefDTO names a container another resource lists: one that uses
// an image or mounts a volume.
type ContainerRefDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ContainerDTO struct {
	ID            string `json:"id"`
	DockerID      string `json:"docker_id"`
	ProjectID     string `json:"project_id,omitempty"`
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

	MemoryReservationBytes   int64 `json:"memory_reservation_bytes"`
	CPUReservationMillicores int64 `json:"cpu_reservation_millicores"`
}

type UserContainerDTO struct {
	ID            string `json:"id"`
	ProjectID     string `json:"project_id,omitempty"`
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

	MemoryReservationBytes   int64 `json:"memory_reservation_bytes"`
	CPUReservationMillicores int64 `json:"cpu_reservation_millicores"`
}

type CreateContainerDTO struct {
	Name         string            `json:"name" binding:"required"`
	ImageTag     string            `json:"image_tag" binding:"required"`
	InternalPort int               `json:"internal_port"`
	EnvVars      map[string]string `json:"env_vars"`
	VolumeMounts []VolumeMountDTO  `json:"volume_mounts"`
	DomainPrefix string            `json:"domain_prefix" binding:"omitempty,max=30"`
	// MemoryReservationMB and CPUReservationMillicores are optional; zero or
	// absent takes the platform default. Core checks their bounds.
	MemoryReservationMB      int64 `json:"memory_reservation_mb"`
	CPUReservationMillicores int64 `json:"cpu_reservation_millicores"`
}

type ExposeContainerDTO struct {
	DomainPrefix string `json:"domain_prefix" binding:"required,max=30"`
	InternalPort int    `json:"internal_port" binding:"required,min=1"`
}

type ContainerStatsDTO struct {
	CPUPercentage            float64 `json:"cpu_percentage"`
	MemoryUsageBytes         int64   `json:"memory_usage_bytes"`
	MemoryLimitBytes         int64   `json:"memory_limit_bytes"`
	MemoryStandardLimitBytes int64   `json:"memory_standard_limit_bytes"`
	NetworkRxBytes           int64   `json:"network_rx_bytes"`
	NetworkTxBytes           int64   `json:"network_tx_bytes"`
}

type PaginatedContainers struct {
	Containers []ContainerDTO `json:"containers"`
	TotalCount int32          `json:"total_count"`
}

// ContainerStatusCountsDTO is how many containers each status filter of the
// containers list shows.
type ContainerStatusCountsDTO struct {
	All       int32 `json:"all"`
	Running   int32 `json:"running"`
	Stopped   int32 `json:"stopped"`
	Attention int32 `json:"attention"`
}
