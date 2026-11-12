package dto

// AdminUserDTO is a user as the admin list and get show it: SSO's identity
// with Core's effective quota. QuotaCustom is false for the default quota.
type AdminUserDTO struct {
	UserID      string  `json:"user_id"`
	Username    string  `json:"username"`
	Email       string  `json:"email"`
	Role        string  `json:"role"`
	Status      string  `json:"status"`
	QuotaCPU    float64 `json:"quota_cpu"`
	QuotaRAMMB  int64   `json:"quota_ram_mb"`
	QuotaDiskMB int64   `json:"quota_disk_mb"`
	QuotaCustom bool    `json:"quota_custom"`
}

type PaginatedAdminUsers struct {
	Users      []AdminUserDTO `json:"users"`
	TotalCount int32          `json:"total_count"`
}

// AdminUserAccountDTO is the user an account change returns. It carries no
// quota: quotas are read with the user and changed through the quota routes.
type AdminUserAccountDTO struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	Status   string `json:"status"`
}

type CreateUserRequest struct {
	Username string `json:"username" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Role     string `json:"role" binding:"required"`
	Status   string `json:"status" binding:"required"`
}

type UpdateUserRequest struct {
	Username string `json:"username" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"omitempty,min=6"`
	Role     string `json:"role" binding:"required"`
	Status   string `json:"status" binding:"required"`
}

// SetUserQuotaRequest is a custom quota: CPU in cores, memory and disk in MB.
type SetUserQuotaRequest struct {
	QuotaCPU    *float64 `json:"quota_cpu" binding:"required"`
	QuotaRAMMB  *int64   `json:"quota_ram_mb" binding:"required"`
	QuotaDiskMB *int64   `json:"quota_disk_mb" binding:"required"`
}

// UserQuotaDTO is a user's effective quota after a set or reset.
type UserQuotaDTO struct {
	UserID      string  `json:"user_id"`
	QuotaCPU    float64 `json:"quota_cpu"`
	QuotaRAMMB  int64   `json:"quota_ram_mb"`
	QuotaDiskMB int64   `json:"quota_disk_mb"`
	QuotaCustom bool    `json:"quota_custom"`
}
