package dto

type BuildDTO struct {
	ID                 string `json:"id"`
	ImageID            string `json:"image_id"`
	ProjectID          string `json:"project_id,omitempty"`
	ProjectServiceName string `json:"project_service_name,omitempty"`
	Status             string `json:"status"`
	StartedAt          int64  `json:"started_at"`
	FinishedAt         int64  `json:"finished_at"`
	LogFilePath        string `json:"log_file_path"`
	OwnerID            string `json:"owner_id"`
	OwnerUsername      string `json:"owner_username"`
}

type UserBuildDTO struct {
	ID                 string `json:"id"`
	ImageID            string `json:"image_id"`
	ProjectID          string `json:"project_id,omitempty"`
	ProjectServiceName string `json:"project_service_name,omitempty"`
	Status             string `json:"status"`
	StartedAt          int64  `json:"started_at"`
	FinishedAt         int64  `json:"finished_at"`
}

type PaginatedBuilds struct {
	Builds     []BuildDTO `json:"builds"`
	TotalCount int32      `json:"total_count"`
}

type BuildImageGitRequest struct {
	RepoURL    string            `json:"repo_url"`
	Ref        string            `json:"ref"`
	Tag        string            `json:"tag"`
	Context    string            `json:"context"`
	Dockerfile string            `json:"dockerfile"`
	BuildArgs  map[string]string `json:"build_args"`
}
