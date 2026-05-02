package model

type AdminUser struct {
	UserID      string
	Username    string
	Email       string
	Role        string
	Status      string
	QuotaCPU    float64
	QuotaRAMMB  int64
	QuotaDiskMB int64
}

type PaginatedAdminUsers struct {
	Users      []AdminUser
	TotalCount int32
}

type CreateUserInput struct {
	Username    string
	Email       string
	Password    string
	Role        string
	Status      string
	QuotaCPU    float64
	QuotaRAMMB  int64
	QuotaDiskMB int64
}

type UpdateUserInput struct {
	Username    string
	Email       string
	Password    string
	Role        string
	Status      string
	QuotaCPU    float64
	QuotaRAMMB  int64
	QuotaDiskMB int64
}
