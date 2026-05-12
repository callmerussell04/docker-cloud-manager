package dto

type ActionCountResponse struct {
	Action string `json:"action"`
	Count  int64  `json:"count"`
}

type ReportsOverviewResponse struct {
	From                         int64                 `json:"from"`
	To                           int64                 `json:"to"`
	AuditEventsTotal             int64                 `json:"audit_events_total"`
	FailedActionsTotal           int64                 `json:"failed_actions_total"`
	ActiveUsersTotal             int64                 `json:"active_users_total"`
	ReservedMemoryBytes          int64                 `json:"reserved_memory_bytes"`
	TotalDiskBytes               int64                 `json:"total_disk_bytes"`
	LastUsageSnapshotAt          int64                 `json:"last_usage_snapshot_at"`
	UsageSnapshotIntervalSeconds int64                 `json:"usage_snapshot_interval_seconds"`
	TopActions                   []ActionCountResponse `json:"top_actions"`
}

type RefreshUsageSnapshotsResponse struct {
	BucketStart    int64 `json:"bucket_start"`
	CollectedAt    int64 `json:"collected_at"`
	SnapshotsCount int   `json:"snapshots_count"`
}

type UserUsageReportItemResponse struct {
	OwnerID             string `json:"owner_id"`
	OwnerUsername       string `json:"owner_username"`
	ReservedMemoryBytes int64  `json:"reserved_memory_bytes"`
	TotalDiskBytes      int64  `json:"total_disk_bytes"`
	ContainersTotal     int    `json:"containers_total"`
	ContainersRunning   int    `json:"containers_running"`
	VolumesTotal        int    `json:"volumes_total"`
	ImagesTotal         int    `json:"images_total"`
	BuildsTotal         int    `json:"builds_total"`
	ProjectsTotal       int    `json:"projects_total"`
	ActionsTotal        int64  `json:"actions_total"`
}

type UserUsageReportResponse struct {
	Users      []UserUsageReportItemResponse `json:"users"`
	TotalCount int                           `json:"total_count"`
}

type UserUsagePointResponse struct {
	BucketStart         int64 `json:"bucket_start"`
	ReservedMemoryBytes int64 `json:"reserved_memory_bytes"`
	TotalDiskBytes      int64 `json:"total_disk_bytes"`
	ContainersTotal     int   `json:"containers_total"`
	ContainersRunning   int   `json:"containers_running"`
	ActionsTotal        int64 `json:"actions_total"`
}

type UserUsageTimelineResponse struct {
	Points []UserUsagePointResponse `json:"points"`
}

type AuditEventResponse struct {
	ID            string `json:"id"`
	OccurredAt    int64  `json:"occurred_at"`
	ActorUserID   string `json:"actor_user_id"`
	ActorUsername string `json:"actor_username"`
	ActorScope    string `json:"actor_scope"`
	Action        string `json:"action"`
	Outcome       string `json:"outcome"`
	ResourceType  string `json:"resource_type"`
	ResourceID    string `json:"resource_id"`
	ResourceName  string `json:"resource_name"`
	OwnerID       string `json:"owner_id"`
	OwnerUsername string `json:"owner_username"`
	RequestID     string `json:"request_id"`
	ClientIP      string `json:"client_ip"`
	UserAgent     string `json:"user_agent"`
	ErrorCode     string `json:"error_code"`
	DetailsJSON   string `json:"details_json"`
}

type AuditEventsResponse struct {
	Events     []AuditEventResponse `json:"events"`
	TotalCount int                  `json:"total_count"`
}
