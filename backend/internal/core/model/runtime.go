package model

import "github.com/google/uuid"

type ContainerMountSpec struct {
	VolumeName string
	Target     string
	ReadOnly   bool
}

type ContainerRuntimeSpec struct {
	ContainerID          string
	OwnerID              string
	ProjectID            string
	Generation           int
	ContainerName        string
	NetworkAlias         string
	ImageName            string
	NetworkName          string
	Domain               string
	InternalPort         int
	EnvVars              []string
	MemoryLimitBytes     int64
	MemoryReservation    int64
	MemorySwapMultiplier float64
	CPUShares            int64
	CPUQuota             int64
	CPUPeriod            int64
	PidsLimit            int64
	ProxyNetworkName     string
	VolumeMounts         []ContainerMountSpec
	MaxLogSize           string
	MaxLogFiles          string
	StorageQuota         string
	Command              []string
	Entrypoint           []string
	Restart              string
	Healthcheck          *Healthcheck
}

type VolumeRuntimeSpec struct {
	VolumeName string
	VolumeID   string
	OwnerID    string
	ProjectID  string
}

type VolumeInspection struct {
	Name       string
	Labels     map[string]string
	Mountpoint string
}

type ContainerInspection struct {
	Name              string
	Image             string
	Env               []string
	Command           []string
	Entrypoint        []string
	MemoryLimitBytes  int64
	MemoryReservation int64
	CPUShares         int64
	CPUQuota          int64
	CPUPeriod         int64
	Restart           string
	Mounts            []ContainerMountSpec
	Healthcheck       *Healthcheck
	State             ContainerState
}

type ContainerResourceUpdate struct {
	MemoryLimitBytes     int64
	MemoryReservation    int64
	MemorySwapMultiplier float64
	CPUShares            int64
	CPUQuota             int64
	CPUPeriod            int64
}

// ContainerUsage is one stats sample of a running container.
type ContainerUsage struct {
	// WorkingSetBytes is memory usage less inactive file cache.
	WorkingSetBytes int64
	// MemoryLimitBytes is the hard memory limit the container runs with now.
	MemoryLimitBytes int64
	// CPUUsageNanoseconds is the CPU time the container has used since it
	// started.
	CPUUsageNanoseconds int64
}

// BuildContainer is a running Kaniko container, identified by the labels
// Builder gives it.
type BuildContainer struct {
	DockerID string
	BuildID  uuid.UUID
	OwnerID  uuid.UUID
}

type ContainerState struct {
	Running      bool
	Status       string
	ExitCode     int
	OOMKilled    bool
	HealthStatus *string
}

type ContainerEvent struct {
	Type        string
	Action      string
	DockerID    string
	ContainerID string
	Generation  int
	ExitCode    *int
	OOMKilled   bool
}
