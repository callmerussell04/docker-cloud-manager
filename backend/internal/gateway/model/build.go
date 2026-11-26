package model

type Build struct {
	ID                 string
	ImageID            string
	Tag                string
	ProjectID          string
	ProjectServiceName string
	// SourceKind is "archive" or "git", or empty for a Compose project's
	// build; SourceLocation is the archive's file name or the repository URL,
	// and SourceRef the Git ref asked for.
	SourceKind     string
	SourceLocation string
	SourceRef      string
	Status         string
	CreatedAt      int64
	StartedAt      int64
	FinishedAt     int64
	LogObjectKey   string
	OwnerID        string
	OwnerUsername  string
}

type PaginatedBuilds struct {
	Builds     []Build
	TotalCount int32
}
