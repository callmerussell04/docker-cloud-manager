package model

type Volume struct {
	ID              string
	Name            string
	DockerName      string
	Status          string
	LastError       string
	UsedBytes       int64
	UsageObservedAt int64
	SizeBytes       int64
	SizeEnforced    bool
	CreatedAt       int64
	OwnerID         string
	OwnerUsername   string
}

type CreateVolumeInput struct {
	Name string
	// SizeBytes is zero for the default size.
	SizeBytes int64
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
