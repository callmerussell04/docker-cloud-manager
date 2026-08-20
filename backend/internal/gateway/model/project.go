package model

type Project struct {
	ID            string
	Name          string
	Status        string
	ErrorMessage  string
	LastError     string
	CreatedAt     int64
	OwnerID       string
	OwnerUsername string
}

type PaginatedProjects struct {
	Projects   []Project
	TotalCount int32
}
