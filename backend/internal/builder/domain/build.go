package domain

import "mime/multipart"

type BuildJob struct {
	OwnerID    string
	Tag        string
	ContextDir string
	Dockerfile string
	File       *multipart.FileHeader
}
