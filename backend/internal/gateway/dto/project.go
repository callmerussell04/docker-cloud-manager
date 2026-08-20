package dto

type ProjectDTO struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	ErrorMessage  string `json:"error_message"`
	LastError     string `json:"last_error"`
	CreatedAt     int64  `json:"created_at"`
	OwnerID       string `json:"owner_id"`
	OwnerUsername string `json:"owner_username"`
}

type UserProjectDTO struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message"`
	LastError    string `json:"last_error"`
	CreatedAt    int64  `json:"created_at"`
}

type PaginatedProjects struct {
	Projects   []ProjectDTO `json:"projects"`
	TotalCount int32        `json:"total_count"`
}
