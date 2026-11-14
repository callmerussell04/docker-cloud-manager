package model

type Build struct {
	ID                 string
	ImageID            string
	ProjectID          string
	ProjectServiceName string
	Status             string
	CreatedAt          int64
	StartedAt          int64
	FinishedAt         int64
	LogObjectKey        string
	OwnerID            string
	OwnerUsername      string
}

type PaginatedBuilds struct {
	Builds     []Build
	TotalCount int32
}
