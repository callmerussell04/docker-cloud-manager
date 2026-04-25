package docker

type MountParam struct {
	VolumeName string
	Target     string
	ReadOnly   bool
}
