package service

type CoreProvider interface {
	ContainerProvider
	VolumeProvider
	ImageProvider
	BuildProvider
	ProjectProvider
	SystemProvider
	StatsProvider
}

type Core struct {
	provider CoreProvider
}

func NewCore(provider CoreProvider) *Core {
	return &Core{
		provider: provider,
	}
}
