package model

import "io"

type Build struct {
	ID                 string
	ImageID            string
	ProjectID          string
	ProjectServiceName string
	Status             string
	StartedAt          int64
	FinishedAt         int64
	LogFilePath        string
	OwnerID            string
	OwnerUsername      string
}

type PaginatedBuilds struct {
	Builds     []Build
	TotalCount int32
}

type BuildArchiveInput struct {
	Tag         string
	ContextDir  string
	Dockerfile  string
	BuildArgs   map[string]string
	ArchiveName string
	Archive     io.Reader
}

type BuildGitInput struct {
	RepoURL    string
	Ref        string
	Tag        string
	ContextDir string
	Dockerfile string
	BuildArgs  map[string]string
}

type BuildInitResult struct {
	BuildID string
}
