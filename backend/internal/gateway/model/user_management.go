package model

// AdminUser is a user's identity as SSO holds it. Quotas are Core's; see
// AdminUserWithQuota.
type AdminUser struct {
	UserID   string
	Username string
	Email    string
	Role     string
	Status   string
}

type PaginatedAdminUsers struct {
	Users      []AdminUser
	TotalCount int32
}

// UserQuota is a user's effective quota from Core: CPU in cores, memory and
// disk in MB. Custom is false when the user has the default quota.
type UserQuota struct {
	UserID string
	CPU    float64
	RAMMB  int64
	DiskMB int64
	Custom bool
}

// UserQuotaLimits is the custom quota an administrator sets.
type UserQuotaLimits struct {
	CPU    float64
	RAMMB  int64
	DiskMB int64
}

// AdminUserWithQuota is what the admin user pages show: SSO's user and
// Core's effective quota for them.
type AdminUserWithQuota struct {
	User  AdminUser
	Quota UserQuota
}

type PaginatedAdminUsersWithQuota struct {
	Users      []AdminUserWithQuota
	TotalCount int32
}

type CreateUserInput struct {
	Username string
	Email    string
	Password string
	Role     string
	Status   string
}

type UpdateUserInput struct {
	Username string
	Email    string
	Password string
	Role     string
	Status   string
}
