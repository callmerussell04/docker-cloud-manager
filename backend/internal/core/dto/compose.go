package dto

import "mime/multipart"

type DeployComposeRequest struct {
	OwnerID     string
	ProjectName string
	Archive     *multipart.FileHeader
}

type DeployComposeGitRequest struct {
	ProjectName string `json:"project_name"`
	RepoURL     string `json:"repo_url"`
	Ref         string `json:"ref"`
	ComposeFile string `json:"compose_file"`
}
