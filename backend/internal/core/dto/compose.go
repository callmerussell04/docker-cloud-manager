package dto

import "mime/multipart"

type DeployComposeRequest struct {
	OwnerID     string
	ProjectName string
	Archive     *multipart.FileHeader
}
