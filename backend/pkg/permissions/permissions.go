package permissions

const (
	SystemConfigRead   = "system.config.read"
	SystemConfigUpdate = "system.config.update"

	ContainersAdminList   = "containers.admin.list"
	ContainersAdminAction = "containers.admin.action"
	ContainersAdminStats  = "containers.admin.stats"

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
	SystemConfigRead:      {},
	SystemConfigUpdate:    {},
	ContainersAdminList:   {},
	ContainersAdminAction: {},
	ContainersAdminStats:  {},
	VolumesAdminList:      {},
	VolumesAdminDelete:    {},
	ImagesAdminList:       {},
	ImagesAdminDelete:     {},
	BuildsAdminList:       {},
	BuildsAdminDelete:     {},
	ProjectsAdminList:     {},
	ProjectsAdminDelete:   {},
	ProjectsAdminStart:    {},
	ProjectsAdminStop:     {},
	UsersAdminList:        {},
	UsersAdminRead:        {},
	UsersAdminCreate:      {},
	UsersAdminUpdate:      {},
	UsersAdminDelete:      {},
}

func Exists(permission string) bool {
	_, ok := all[permission]
	return ok
}
