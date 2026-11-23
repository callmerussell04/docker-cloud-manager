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
	// UsedBy names the containers created from the image; never null.
	UsedBy []ImageUserDTO `json:"used_by"`
}

// ImageUserDTO is a container created from an image.
type ImageUserDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type UserImageDTO struct {
	ID        string `json:"id"`
	Tag       string `json:"tag"`
	SizeBytes int64  `json:"size_bytes"`
	Status    string `json:"status"`
	LastError string `json:"last_error"`
	CreatedAt int64  `json:"created_at"`
	// UsedBy names the containers created from the image; never null.
	UsedBy []ImageUserDTO `json:"used_by"`
}

type PaginatedImages struct {
	Images     []ImageDTO `json:"images"`
	TotalCount int32      `json:"total_count"`
}
