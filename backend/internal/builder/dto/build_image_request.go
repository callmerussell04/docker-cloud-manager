package dto

type BuildImageRequest struct {
	OwnerID    string
	Tag        string
	ContextDir string
	Dockerfile string
	BuildArgs  map[string]string
}
