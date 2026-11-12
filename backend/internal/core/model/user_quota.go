package model

// UserQuota is what one owner may reserve: CPU in millicores, memory and disk
// in MB. Custom marks a quota an administrator set for that owner; any other
// owner has the default quota from the runtime config.
type UserQuota struct {
	CPUMillicores int64
	RAMMB         int64
	DiskMB        int64
	Custom        bool
}
