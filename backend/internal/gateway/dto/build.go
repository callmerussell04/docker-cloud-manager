package dto

type BuildDTO struct {
	ID                 string `json:"id"`
	ImageID            string `json:"image_id,omitempty"`
	Tag                string `json:"tag"`
	ProjectID          string `json:"project_id,omitempty"`
	ProjectServiceName string `json:"project_service_name,omitempty"`
	SourceKind         string `json:"source_kind,omitempty"`
	SourceLocation     string `json:"source_location,omitempty"`
	SourceRef          string `json:"source_ref,omitempty"`
	Status             string `json:"status"`
	CreatedAt          int64  `json:"created_at"`
	StartedAt          int64  `json:"started_at"`
	FinishedAt         int64  `json:"finished_at"`
	LogObjectKey       string `json:"log_object_key"`
	OwnerID            string `json:"owner_id"`
	OwnerUsername      string `json:"owner_username"`
}

type UserBuildDTO struct {
	ID                 string `json:"id"`
	ImageID            string `json:"image_id,omitempty"`
	Tag                string `json:"tag"`
	ProjectID          string `json:"project_id,omitempty"`
	ProjectServiceName string `json:"project_service_name,omitempty"`
	SourceKind         string `json:"source_kind,omitempty"`
	SourceLocation     string `json:"source_location,omitempty"`
	SourceRef          string `json:"source_ref,omitempty"`
	Status             string `json:"status"`
	CreatedAt          int64  `json:"created_at"`
	StartedAt          int64  `json:"started_at"`
	FinishedAt         int64  `json:"finished_at"`
}

type PaginatedBuilds struct {
	Builds     []BuildDTO `json:"builds"`
	TotalCount int32      `json:"total_count"`
}
