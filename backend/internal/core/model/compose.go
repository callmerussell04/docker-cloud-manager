package model

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
	DependsOn    map[string]string
	Command      []string
	Entrypoint   []string
	BuildArgs    map[string]string
	Restart      string
	Healthcheck  *Healthcheck

	// Роутинг (заполняется только если есть кастомные лейблы)
	DomainPrefix string
	InternalPort int
}
