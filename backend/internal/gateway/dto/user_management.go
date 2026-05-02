package dto

type AdminUserDTO struct {
	UserID      string  `json:"user_id"`
	Username    string  `json:"username"`
	Email       string  `json:"email"`
	Role        string  `json:"role"`
	Status      string  `json:"status"`
	QuotaCPU    float64 `json:"quota_cpu"`
	QuotaRAMMB  int64   `json:"quota_ram_mb"`
	QuotaDiskMB int64   `json:"quota_disk_mb"`
}

type PaginatedAdminUsers struct {
	Users      []AdminUserDTO `json:"users"`
	TotalCount int32          `json:"total_count"`
}

type CreateUserRequest struct {
	Username    string  `json:"username" binding:"required"`
	Email       string  `json:"email" binding:"required,email"`
	Password    string  `json:"password" binding:"required,min=6"`
	Role        string  `json:"role" binding:"required"`
	Status      string  `json:"status" binding:"required"`
	QuotaCPU    float64 `json:"quota_cpu" binding:"required,gt=0"`
	QuotaRAMMB  int64   `json:"quota_ram_mb" binding:"required,gt=0"`
	QuotaDiskMB int64   `json:"quota_disk_mb" binding:"required,gt=0"`
}

type UpdateUserRequest struct {
	Username    string  `json:"username" binding:"required"`
	Email       string  `json:"email" binding:"required,email"`
	Password    string  `json:"password" binding:"omitempty,min=6"`
	Role        string  `json:"role" binding:"required"`
	Status      string  `json:"status" binding:"required"`
	QuotaCPU    float64 `json:"quota_cpu" binding:"required,gt=0"`
	QuotaRAMMB  int64   `json:"quota_ram_mb" binding:"required,gt=0"`
	QuotaDiskMB int64   `json:"quota_disk_mb" binding:"required,gt=0"`
}
