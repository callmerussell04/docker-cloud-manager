package model

type ActionCount struct {
	Action string
	Count  int64
}

type ReportsOverview struct {
	From                         int64
	To                           int64
	AuditEventsTotal             int64
	FailedActionsTotal           int64
	ActiveUsersTotal             int64
	MemoryUsageBytes             int64
	ReservedMemoryBytes          int64
	CPUPercent                   float64
	TotalDiskBytes               int64
	ResourcesTotal               int64
	ContainersTotal              int64
	ContainersRunning            int64
	VolumesTotal                 int64
	ImagesTotal                  int64
	BuildsTotal                  int64
	ProjectsTotal                int64
	LastUsageSnapshotAt          int64
	UsageSnapshotIntervalSeconds int64
	TopActions                   []ActionCount
}

type RefreshUsageSnapshotsResult struct {
	BucketStart    int64
	CollectedAt    int64
	SnapshotsCount int
}

type UserUsageReportItem struct {
	OwnerID             string
	OwnerUsername       string
	MemoryUsageBytes    int64
	ReservedMemoryBytes int64
	CPUPercent          float64
	TotalDiskBytes      int64
	ResourcesTotal      int
	ContainersTotal     int
	ContainersRunning   int
	VolumesTotal        int
	ImagesTotal         int
	BuildsTotal         int
	ProjectsTotal       int
	ActionsTotal        int64
}

type UserUsagePoint struct {
	BucketStart         int64
	MemoryUsageBytes    int64
	ReservedMemoryBytes int64
	CPUPercent          float64
	TotalDiskBytes      int64
	ResourcesTotal      int
	ContainersTotal     int
	ContainersRunning   int
	VolumesTotal        int
	ImagesTotal         int
	BuildsTotal         int
	ProjectsTotal       int
	ActionsTotal        int64
}

type AuditEvent struct {
	ID            string
	OccurredAt    int64
	ActorUserID   string
	ActorUsername string
	ActorScope    string
	Action        string
	Outcome       string
	ResourceType  string
	ResourceID    string
	ResourceName  string
	OwnerID       string
	OwnerUsername string
	RequestID     string
	ClientIP      string
	UserAgent     string
	ErrorCode     string
	DetailsJSON   string
}

type AuditEventFilters struct {
	From         int64
	To           int64
	ActorUserID  string
	Action       string
	Outcome      string
	ResourceType string
	Search       string
	Page         int
	Limit        int
}
