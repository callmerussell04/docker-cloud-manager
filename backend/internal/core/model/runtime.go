package model

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
