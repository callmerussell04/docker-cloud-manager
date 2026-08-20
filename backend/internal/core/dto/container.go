package dto

import "github.com/google/uuid"

type CreateContainerDTO struct {
	Name         string
	ImageTag     string
	InternalPort int
	EnvVars      map[string]string
	VolumeMounts []VolumeMountDTO
	DomainPrefix string
}

type VolumeMountDTO struct {
	VolumeID   uuid.UUID
	MountPath  string
	IsReadOnly bool
}
