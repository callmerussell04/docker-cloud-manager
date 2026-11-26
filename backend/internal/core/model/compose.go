package model

type ComposeProject struct {
	Name     string
	Services []ComposeService
	Volumes  []ComposeVolume
}

type GitSource struct {
	RepoURL     string
	Ref         string
	ComposeFile string
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

	// MemoryReservationMB and CPUReservationMillicores are the service's
	// reservation from the Compose file; zero takes the default.
	MemoryReservationMB      int64
	CPUReservationMillicores int64

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

// ComposePlanDemand is what a Compose deployment's plan asks of its owner and
// of the host, admitted once at ingestion. Nothing is reserved by it: each
// later create, volume create, build and start is admitted again.
type ComposePlanDemand struct {
	// Services is how many containers the plan creates.
	Services int
	// MemoryBytes and CPUMillicores sum the services' reservations: each
	// service's own figure, or the default.
	MemoryBytes   int64
	CPUMillicores int64
	// ManagedVolumeNames are the distinct names of the plan's managed
	// volumes. A name the owner already has is reused; every other one is
	// created at the default volume size.
	ManagedVolumeNames []string
	// Builds is how many image builds the plan queues.
	Builds int
}
