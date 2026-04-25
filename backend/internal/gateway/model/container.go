package model

type Container struct {
	ID            string
	DockerID      string
	Name          string
	ImageTag      string
	InternalPort  int32
	DomainPrefix  string
	Status        string
	CreatedAt     int64
	OwnerID       string
	OwnerUsername string
}

type CreateContainerInput struct {
	Name         string
	ImageTag     string
	InternalPort int
	EnvVars      map[string]string
	VolumeMounts []VolumeMountInput
	DomainPrefix string
}

type ContainerStats struct {
	CPUPercentage    float64
	MemoryUsageBytes int64
	MemoryLimitBytes int64
	NetworkRxBytes   int64
	NetworkTxBytes   int64
}

type PaginatedContainers struct {
	Containers []Container
	TotalCount int32
}
