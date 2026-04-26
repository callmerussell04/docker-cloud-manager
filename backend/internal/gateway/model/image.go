package model

type Image struct {
	ID            string
	Tag           string
	SizeMB        int32
	IsCustom      bool
	Status        string
	LastError     string
	CreatedAt     int64
	OwnerID       string
	OwnerUsername string
}

type PaginatedImages struct {
	Images     []Image
	TotalCount int32
}
