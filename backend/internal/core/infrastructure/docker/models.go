package docker

type CreateContainerParams struct {
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
	VolumeMounts      []MountParam
	MaxLogSize        string
	MaxLogFiles       string
	StorageQuota      string
}

type MountParam struct {
	VolumeName string
	Target     string
	ReadOnly   bool
}

type CreateVolumeParams struct {
	VolumeName string
	Driver     string
	DriverOpts map[string]string
}
