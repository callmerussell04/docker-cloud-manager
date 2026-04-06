package domain

type ComposeProject struct {
	Name     string
	Services []ComposeService
	Volumes  []VolumeCreateParams
}

type ComposeService struct {
	Name         string
	ImageTag     string
	BuildContext string
	Dockerfile   string
	EnvVars      map[string]string
	VolumeMounts []VolumeMountParams
	DependsOn    []string

	// Роутинг (заполняется только если есть кастомные лейблы)
	DomainPrefix string
	InternalPort int
}
