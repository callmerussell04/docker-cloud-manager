package docker

import "github.com/callmerussell04/docker-cloud-manager/internal/core/model"

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
	Command           []string
	Entrypoint        []string
	Restart           string
	Healthcheck       *model.Healthcheck
}
