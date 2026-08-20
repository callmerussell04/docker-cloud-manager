package model

type BuildRuntimeSpec struct {
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
}
