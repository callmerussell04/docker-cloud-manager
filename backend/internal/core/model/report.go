package model

import (
	"time"

	"github.com/google/uuid"
)

type AuditEvent struct {
	ID            uuid.UUID
	OccurredAt    time.Time
	ActorUserID   *uuid.UUID
	ActorUsername string
	ActorScope    string
	Action        string
	Outcome       string
	ResourceType  string
	ResourceID    string
	ResourceName  string
	OwnerID       *uuid.UUID
	OwnerUsername string
	RequestID     string
	ClientIP      string
	UserAgent     string
	ErrorCode     string
	DetailsJSON   string
}

type UsageSnapshot struct {
	OwnerID             uuid.UUID
	OwnerUsername       string
	BucketStart         time.Time
	CollectedAt         time.Time
	ReservedMemoryBytes int64
	ImageDiskBytes      int64
	VolumeDiskBytes     int64
	TotalDiskBytes      int64
	ContainersTotal     int
	ContainersRunning   int
	VolumesTotal        int
	ImagesTotal         int
	BuildsTotal         int
	ProjectsTotal       int
}

type ReportsOverview struct {
	From                time.Time
	To                  time.Time
	AuditEventsTotal    int64
	FailedActionsTotal  int64
	ActiveUsersTotal    int64
	ReservedMemoryBytes int64
	TotalDiskBytes      int64
	TopActions          []ActionCount
}

type ActionCount struct {
	Action string
	Count  int64
}

type UserUsageReportItem struct {
	OwnerID             uuid.UUID
	OwnerUsername       string
	ReservedMemoryBytes int64
	TotalDiskBytes      int64
	ContainersTotal     int
	ContainersRunning   int
	VolumesTotal        int
	ImagesTotal         int
	BuildsTotal         int
	ProjectsTotal       int
	ActionsTotal        int64
}

type UserUsagePoint struct {
	BucketStart         time.Time
	ReservedMemoryBytes int64
	TotalDiskBytes      int64
	ContainersTotal     int
	ContainersRunning   int
	ActionsTotal        int64
}

type AuditEventFilters struct {
	From         time.Time
	To           time.Time
	ActorUserID  *uuid.UUID
	Action       string
	Outcome      string
	ResourceType string
	Search       string
	Limit        int
	Offset       int
}
