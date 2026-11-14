package dto

type ImageDTO struct {
	ID            string `json:"id"`
	Tag           string `json:"tag"`
	SizeBytes     int64  `json:"size_bytes"`
	Status        string `json:"status"`
	LastError     string `json:"last_error"`
	CreatedAt     int64  `json:"created_at"`
	OwnerID       string `json:"owner_id"`
	OwnerUsername string `json:"owner_username"`
}

type UserImageDTO struct {
	ID        string `json:"id"`
	Tag       string `json:"tag"`
	SizeBytes int64  `json:"size_bytes"`
	Status    string `json:"status"`
	LastError string `json:"last_error"`
	CreatedAt int64  `json:"created_at"`
}

type PaginatedImages struct {
	Images     []ImageDTO `json:"images"`
	TotalCount int32      `json:"total_count"`
}
