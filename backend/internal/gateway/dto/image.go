package dto

type ImageDTO struct {
	ID            string `json:"id"`
	Tag           string `json:"tag"`
	SizeMB        int32  `json:"size_mb"`
	IsCustom      bool   `json:"is_custom"`
	Status        string `json:"status"`
	LastError     string `json:"last_error"`
	CreatedAt     int64  `json:"created_at"`
	OwnerID       string `json:"owner_id"`
	OwnerUsername string `json:"owner_username"`
}

type RegisterImageDTO struct {
	Tag    string `json:"tag" binding:"required"`
	SizeMB int    `json:"size_mb" binding:"required,min=1"`
}

type PaginatedImages struct {
	Images     []ImageDTO `json:"images"`
	TotalCount int32      `json:"total_count"`
}
