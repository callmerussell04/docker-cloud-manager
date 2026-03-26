package domain

import "github.com/google/uuid"

type ContainerCreateParams struct {
	Name         string
	ImageTag     string
	InternalPort int
	EnvVars      map[string]string
	VolumeMounts []VolumeMountParams
}

type VolumeMountParams struct {
	VolumeID   uuid.UUID
	MountPath  string
	IsReadOnly bool
}

type VolumeCreateParams struct {
	Name       string
	Driver     string
	DriverOpts map[string]string
}
