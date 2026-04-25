package docker

type BuildContainerParams struct {
	WorkspaceDir   string
	ContextSubDir  string
	Dockerfile     string
	DestinationTag string
	MemoryBytes    int64
	CPUQuota       int64
	BuildArgs      map[string]string
	NetworkName    string
}
