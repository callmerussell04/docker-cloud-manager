package permissions

const (
	SystemConfigRead     = "system.config.read"
	SystemConfigUpdate   = "system.config.update"
	SystemMonitoringRead = "system.monitoring.read"
	ReportsAdminRead     = "reports.admin.read"

	ContainersAdminList     = "containers.admin.list"
	ContainersAdminAction   = "containers.admin.action"
	ContainersAdminStats    = "containers.admin.stats"
	ContainersAdminLogs     = "containers.admin.logs"
	ContainersAdminTerminal = "containers.admin.terminal"

	VolumesAdminList   = "volumes.admin.list"
	VolumesAdminDelete = "volumes.admin.delete"

	ImagesAdminList   = "images.admin.list"
	ImagesAdminDelete = "images.admin.delete"

	BuildsAdminList   = "builds.admin.list"
	BuildsAdminDelete = "builds.admin.delete"

	ProjectsAdminList   = "projects.admin.list"
	ProjectsAdminDelete = "projects.admin.delete"
	ProjectsAdminStart  = "projects.admin.start"
	ProjectsAdminStop   = "projects.admin.stop"

	UsersAdminList   = "users.admin.list"
	UsersAdminRead   = "users.admin.read"
	UsersAdminCreate = "users.admin.create"
	UsersAdminUpdate = "users.admin.update"
	UsersAdminDelete = "users.admin.delete"
)

var all = map[string]struct{}{
	SystemConfigRead:        {},
	SystemConfigUpdate:      {},
	SystemMonitoringRead:    {},
	ReportsAdminRead:        {},
	ContainersAdminList:     {},
	ContainersAdminAction:   {},
	ContainersAdminStats:    {},
	ContainersAdminLogs:     {},
	ContainersAdminTerminal: {},
	VolumesAdminList:        {},
	VolumesAdminDelete:      {},
	ImagesAdminList:         {},
	ImagesAdminDelete:       {},
	BuildsAdminList:         {},
	BuildsAdminDelete:       {},
	ProjectsAdminList:       {},
	ProjectsAdminDelete:     {},
	ProjectsAdminStart:      {},
	ProjectsAdminStop:       {},
	UsersAdminList:          {},
	UsersAdminRead:          {},
	UsersAdminCreate:        {},
	UsersAdminUpdate:        {},
	UsersAdminDelete:        {},
}

func Exists(permission string) bool {
	_, ok := all[permission]
	return ok
}
