package domain

import "github.com/google/uuid"

type ContainerCreateParams struct {
	ProjectID         *uuid.UUID
	Name              string
	NetworkAlias      string
	ImageTag          string
	InternalPort      int
	EnvVars           map[string]string
	VolumeMounts      []VolumeMountParams
	RequestedMemoryMB int64
	DomainPrefix      string

	Command     []string
	Entrypoint  []string
	Restart     string
	Healthcheck *Healthcheck
}

type VolumeMountParams struct {
	VolumeID   uuid.UUID
	VolumeName string
	MountPath  string
	IsReadOnly bool
}

type VolumeCreateParams struct {
	ProjectID  *uuid.UUID
	Name       string
	Driver     string
	DriverOpts map[string]string
}
