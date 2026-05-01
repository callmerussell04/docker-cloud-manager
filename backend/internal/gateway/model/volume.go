package model

type Volume struct {
	ID              string
	DockerName      string
	Driver          string
	Status          string
	LastError       string
	UsedBytes       int64
	UsageObservedAt int64
	CreatedAt       int64
	OwnerID         string
	OwnerUsername   string
}

type CreateVolumeInput struct {
	Name string
}

type VolumeMountInput struct {
	VolumeID   string
	MountPath  string
	IsReadOnly bool
}

type PaginatedVolumes struct {
	Volumes    []Volume
	TotalCount int32
}
