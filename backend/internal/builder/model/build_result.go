package model

// BuildResult contains only the outcome needed to finalize an executed build.
// Credentials and source configuration must never enter the result journal.
type BuildResult struct {
	ImageID string
	Status  BuildStatus
	SizeMB  int
}
