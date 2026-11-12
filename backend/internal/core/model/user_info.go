package model

import "github.com/google/uuid"

type UserInfo struct {
	Status      string
	ID          uuid.UUID
	Username    string
	Role        string
	QuotaRAMMB  int64
	QuotaDiskMB int64
	QuotaCPU    float64
}

// BytesToMBRoundedUp converts bytes to the whole MB that disk quotas count,
// rounding a partial MB up. Zero and negative sizes count as zero.
func BytesToMBRoundedUp(bytes int64) int64 {
	if bytes <= 0 {
		return 0
	}
	const mb = 1024 * 1024
	return (bytes + mb - 1) / mb
}
