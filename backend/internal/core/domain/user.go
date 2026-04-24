package domain

import "github.com/google/uuid"

type UserInfo struct {
	ID          uuid.UUID
	Username    string
	Role        string
	QuotaRAMMB  int64
	QuotaDiskMB int64
	QuotaCPU    float64
}
