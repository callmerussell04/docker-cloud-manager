package model

// AdminUser is a user's identity as SSO holds it. Quotas are Core's; see
// AdminUserWithQuota.
type AdminUser struct {
	UserID   string
	Username string
	Email    string
	Role     string
	Status   string
	// AuthSource is "local" or "oidc"; the provider owns an OIDC user's
	// username, email and password.
	AuthSource string
	// RoleManagedByProvider is true when the provider owns the role.
	RoleManagedByProvider bool
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

// WorkloadSuspension is what Core did to a deactivated user's workloads.
// Skipped containers had another operation in progress; failed workloads
// could not be stopped or canceled this time.
type WorkloadSuspension struct {
	StoppedContainers   int
	SkippedContainers   int
	CanceledBuilds      int
	CanceledDeployments int
	Failed              int
}

// Remaining is how many workloads repeating the deactivation would still
// stop or cancel.
func (s WorkloadSuspension) Remaining() int {
	return s.SkippedContainers + s.Failed
}

// UserDeactivation is a deactivated user and what happened to their workloads.
type UserDeactivation struct {
	User      AdminUser
	Workloads WorkloadSuspension
}

// UserUpdate is an updated user. An update that leaves the account
// deactivated also stops the user's workloads: Deactivated is then true and
// Workloads says what happened to them.
type UserUpdate struct {
	User        AdminUser
	Deactivated bool
	Workloads   WorkloadSuspension
}
