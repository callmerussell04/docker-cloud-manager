package dto

import "mime/multipart"

type BuildImageRequest struct {
	OwnerID    string
	Tag        string
	ContextDir string
	Dockerfile string
	File       *multipart.FileHeader
	BuildArgs  map[string]string
}
