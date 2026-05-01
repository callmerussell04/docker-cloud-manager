package dto

type BuildDTO struct {
	ID            string `json:"id"`
	ImageID       string `json:"image_id"`
	Status        string `json:"status"`
	StartedAt     int64  `json:"started_at"`
	FinishedAt    int64  `json:"finished_at"`
	LogFilePath   string `json:"log_file_path"`
	OwnerID       string `json:"owner_id"`
	OwnerUsername string `json:"owner_username"`
}

type UserBuildDTO struct {
	ID         string `json:"id"`
	ImageID    string `json:"image_id"`
	Status     string `json:"status"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at"`
}

type PaginatedBuilds struct {
	Builds     []BuildDTO `json:"builds"`
	TotalCount int32      `json:"total_count"`
}
