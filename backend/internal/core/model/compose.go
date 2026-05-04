package model

type ComposeProject struct {
	Name     string
	Services []ComposeService
	Volumes  []VolumeCreateParams
}

const (
	ComposeDependencyConditionStarted               = "service_started"
	ComposeDependencyConditionHealthy               = "service_healthy"
	ComposeDependencyConditionCompletedSuccessfully = "service_completed_successfully"
)

type ComposeService struct {
	Name         string
	ImageTag     string
	BuildContext string
	Dockerfile   string
	EnvVars      map[string]string
	VolumeMounts []VolumeMountParams
	DependsOn    []ComposeDependency
	Command      []string
	Entrypoint   []string
	BuildArgs    map[string]string
	Restart      string
	Healthcheck  *Healthcheck

	// Роутинг (заполняется только если есть кастомные лейблы)
	DomainPrefix string
	InternalPort int
}

type ComposeDependency struct {
	ServiceName string
	Condition   string
	Optional    bool
}

func IsValidComposeDependencyCondition(condition string) bool {
	switch condition {
	case ComposeDependencyConditionStarted,
		ComposeDependencyConditionHealthy,
		ComposeDependencyConditionCompletedSuccessfully:
		return true
	default:
		return false
	}
}
