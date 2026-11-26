package model

// ContainerRef names a container another resource lists: one that uses an
// image or mounts a volume.
type ContainerRef struct {
	ContainerID string
	Name        string
}

type Container struct {
	ID            string
	DockerID      string
	ProjectID     string
	Name          string
	ImageTag      string
	InternalPort  int32
	DomainPrefix  string
	Status        string
	DesiredStatus string
	LastError     string
	LastExitCode  *int
	TTLDeadline   int64
	CreatedAt     int64
	OwnerID       string
	OwnerUsername string

	MemoryReservationBytes   int64
	CPUReservationMillicores int64
}

type CreateContainerInput struct {
	Name         string
	ImageTag     string
	InternalPort int
	EnvVars      map[string]string
	VolumeMounts []VolumeMountInput
	DomainPrefix string

	MemoryReservationMB      int64
	CPUReservationMillicores int64
}

type ContainerStats struct {
	CPUPercentage    float64
	MemoryUsageBytes int64
	MemoryLimitBytes int64
	// MemoryStandardLimitBytes is the limit create and start apply; a
	// current limit above it is borrowed.
	MemoryStandardLimitBytes int64
	NetworkRxBytes           int64
	NetworkTxBytes           int64
}

// ContainerListParams is a page of the containers list: the page, the
// project, and the status filter, sort, order and search, each empty when
// omitted.
type ContainerListParams struct {
	Page      int
	Limit     int
	ProjectID string
	Status    string
	Sort      string
	Order     string
	Query     string
}

// ContainerStatusCounts is how many containers each status filter shows.
type ContainerStatusCounts struct {
	All       int32
	Running   int32
	Stopped   int32
	Attention int32
}

type PaginatedContainers struct {
	Containers   []Container
	TotalCount   int32
	StatusCounts ContainerStatusCounts
}
