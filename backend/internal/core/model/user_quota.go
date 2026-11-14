package model

import "math"

// UserQuota is what one owner may reserve: CPU in millicores, memory and disk
// in MB. Custom marks a quota an administrator set for that owner; any other
// owner has the default quota from the runtime config.
type UserQuota struct {
	CPUMillicores int64
	RAMMB         int64
	DiskMB        int64
	Custom        bool
}

// DiskBytes is the disk quota in bytes, the unit reserved disk is counted in.
// A quota too large to express in bytes is the largest byte count.
func (q UserQuota) DiskBytes() int64 {
	const bytesPerMB = 1024 * 1024
	if q.DiskMB > math.MaxInt64/bytesPerMB {
		return math.MaxInt64
	}
	return max(q.DiskMB, 0) * bytesPerMB
}

// UserQuotaLimits is a custom quota as an administrator asks for it: CPU in
// cores, memory and disk in MB.
type UserQuotaLimits struct {
	CPUCores float64
	RAMMB    int64
	DiskMB   int64
}
