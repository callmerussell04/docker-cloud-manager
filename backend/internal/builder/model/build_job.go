package model

type BuildJob struct {
	OwnerID    string
	Tag        string
	ContextDir string
	Dockerfile string
	BuildArgs  map[string]string
}
