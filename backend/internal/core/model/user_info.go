package model

import "github.com/google/uuid"

// UserInfo is the identity Core reads from SSO. Quotas are not part of it:
// Core owns them (see model.UserQuota).
type UserInfo struct {
	Status   string
	ID       uuid.UUID
	Username string
	Role     string
}

// BytesToMBRoundedUp converts bytes to the whole MB that disk quotas count,
// rounding a partial MB up. Zero and negative sizes count as zero.
func BytesToMBRoundedUp(bytes int64) int64 {
	if bytes <= 0 {
		return 0
	}
	return (bytes + BytesPerMB - 1) / BytesPerMB
}
