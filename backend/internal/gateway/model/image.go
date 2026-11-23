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
	// UsedBy names the containers created from the image.
	UsedBy []ImageUser
}

// ImageUser is a container created from an image.
type ImageUser struct {
	ContainerID string
	Name        string
}

type PaginatedImages struct {
	Images     []Image
	TotalCount int32
}
