package model

type ContainerMountSpec struct {
	VolumeName string
	Target     string
	ReadOnly   bool
}

type ContainerRuntimeSpec struct {
	ContainerName     string
	NetworkAlias      string
	ImageName         string
	NetworkName       string
	Domain            string
	InternalPort      int
	EnvVars           []string
	MemoryLimitBytes  int64
	MemoryReservation int64
	CPUShares         int64
	VolumeMounts      []ContainerMountSpec
	MaxLogSize        string
	MaxLogFiles       string
	StorageQuota      string
	Command           []string
	Entrypoint        []string
	Restart           string
	Healthcheck       *Healthcheck
}

type VolumeRuntimeSpec struct {
	VolumeName string
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
	Restart           string
	Mounts            []ContainerMountSpec
	Healthcheck       *Healthcheck
	State             ContainerState
}

type ContainerState struct {
	Running      bool
	Status       string
	ExitCode     int
	HealthStatus *string
}

type ContainerEvent struct {
	Type     string
	Action   string
	DockerID string
}
