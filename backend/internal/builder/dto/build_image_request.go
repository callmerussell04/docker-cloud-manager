package dto

type BuildImageRequest struct {
	Tag        string
	ContextDir string
	Dockerfile string
	BuildArgs  map[string]string
}
