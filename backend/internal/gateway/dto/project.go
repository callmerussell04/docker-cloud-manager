package dto

type ProjectDTO struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	ErrorMessage  string `json:"error_message"`
	CreatedAt     int64  `json:"created_at"`
	OwnerID       string `json:"owner_id"`
	OwnerUsername string `json:"owner_username"`
}

type PaginatedProjects struct {
	Projects   []ProjectDTO `json:"projects"`
	TotalCount int32        `json:"total_count"`
}
