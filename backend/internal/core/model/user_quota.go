package model

import "math"

// BytesPerMB is one MB as quotas and reservations count it: a mebibyte.
const BytesPerMB = int64(1 << 20)

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
	if q.DiskMB > math.MaxInt64/BytesPerMB {
		return math.MaxInt64
	}
	return max(q.DiskMB, 0) * BytesPerMB
}

// UserQuotaLimits is a custom quota as an administrator asks for it: CPU in
// cores, memory and disk in MB.
type UserQuotaLimits struct {
	CPUCores float64
	RAMMB    int64
	DiskMB   int64
}
