package dto

type BuildImageGitRequest struct {
	RepoURL    string            `json:"repo_url"`
	Ref        string            `json:"ref"`
	Tag        string            `json:"tag"`
	Context    string            `json:"context"`
	Dockerfile string            `json:"dockerfile"`
	BuildArgs  map[string]string `json:"build_args"`
}
