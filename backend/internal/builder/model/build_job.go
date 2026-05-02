package model

type BuildJob struct {
	Tag        string
	ContextDir string
	Dockerfile string
	BuildArgs  map[string]string
}
