package domain

import "mime/multipart"

type BuildJob struct {
	OwnerID string
	Tag     string
	File    *multipart.FileHeader
}
