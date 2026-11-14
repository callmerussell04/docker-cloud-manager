package model

type Image struct {
	ID            string
	Tag           string
	SizeBytes     int64
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
