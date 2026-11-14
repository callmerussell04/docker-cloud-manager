package model

type BuildRuntimeSpec struct {
	RegistryUsername     string
	RegistryPassword     string
	BuildID              string
	OwnerID              string
	WorkspaceDir         string
	ContextSubDir        string
	Dockerfile           string
	DestinationTag       string
	KanikoImage          string
	MemoryBytes          int64
	MemorySwapMultiplier float64
	CPUQuota             int64
	CPUPeriod            int64
	PidsLimit            int64
	BuildArgs            map[string]string
	NetworkName          string
	// MaxDiskBytes is the root filesystem size Docker is asked to cap the
	// build container at. Zero asks for no cap.
	MaxDiskBytes int64
}

// BuildClaim carries per-attempt authority only on Core's trusted response.
type BuildClaim struct {
	ImageID          string
	RegistryUsername string
	RegistryPassword string
	Destination      string
}
