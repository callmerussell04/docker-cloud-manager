package model

// WorkloadSuspension is what suspending an owner's workloads did: the
// containers it queued a stop for, the builds and Compose deployments it
// canceled, the containers it skipped because another operation was active,
// and the workloads it failed to stop or cancel. Skipped and failed
// workloads remain; suspending again handles them.
type WorkloadSuspension struct {
	StoppedContainers   int
	SkippedContainers   int
	CanceledBuilds      int
	CanceledDeployments int
	Failed              int
}

// Remaining is how many workloads suspending again would still act on.
func (s WorkloadSuspension) Remaining() int {
	return s.SkippedContainers + s.Failed
}
