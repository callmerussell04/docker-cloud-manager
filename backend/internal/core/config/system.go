package config

import "math"

const (
	DefaultReportsUsageSnapshotIntervalSeconds = int64(300)
	MinReportsUsageSnapshotIntervalSeconds     = int64(60)
	MaxReportsUsageSnapshotIntervalSeconds     = int64(86400)
	MaxVolumeUsageScanEntryBudget              = 10_000_000
	// MaxBoostMultiplier bounds MemoryBoostMultiplier and CPUBoostMultiplier.
	MaxBoostMultiplier = int64(64)
	// MinVolumeSizeBytes is the smallest volume size, for the default and for
	// a size given at creation.
	MinVolumeSizeBytes = int64(1 << 20)
	// MaxUserQuotaCPU bounds a CPU quota in cores, so its millicores stay an
	// exact int64.
	MaxUserQuotaCPU = 1_000_000.0
	// DefaultBuildMaxDiskBytes and MinBuildMaxDiskBytes are the default and
	// the smallest disk one build may write.
	DefaultBuildMaxDiskBytes = int64(10 << 30)
	MinBuildMaxDiskBytes     = int64(64 << 20)
)

// CPUMillicores converts a CPU quota in cores to the whole millicores that
// admission counts, rounding to the nearest millicore.
func CPUMillicores(cores float64) int64 {
	return int64(math.Round(cores * 1000))
}

// SystemConfig is the configuration snapshot Core reads before each operation
// or worker tick. Its own fields are the runtime settings: administrators read
// and change them through /admin/config and they persist in the config file.
// The embedded Fixed part is set at startup and never changes while Core runs.
type SystemConfig struct {
	// Capacity of the server and how much of it users may be promised.
	ReservedSystemMemory int64   `json:"reserved_system_memory_bytes"`
	OvercommitFactor     float64 `json:"overcommit_factor"`
	ReservedSystemCPU    int64   `json:"reserved_system_cpu_millicores"`
	CPUOvercommitFactor  float64 `json:"cpu_overcommit_factor"`
	HostMinFreeDiskBytes int64   `json:"host_min_free_disk_bytes"`

	// Container memory and CPU: the default reservation, the standard limit
	// as a multiple of it, and the boost ceiling a running container may
	// borrow up to.
	DefaultMemoryReservation int64 `json:"default_memory_reservation_bytes"`
	MaxBurstMultiplier       int64 `json:"max_burst_multiplier"`
	DefaultCPUReservation    int64 `json:"default_cpu_reservation_millicores"`
	MaxCPUBurstMultiplier    int64 `json:"max_cpu_burst_multiplier"`
	// MemoryPressureMinFreeBytes is the free part of the user memory pool
	// below which the memory pressure worker stops workloads. Zero turns the
	// worker, and with it every boost, off.
	MemoryPressureMinFreeBytes int64 `json:"memory_pressure_min_free_bytes"`
	// MemoryBoostMultiplier times the memory reservation is the boost
	// ceiling a running container's limit may be raised to. At or below
	// MaxBurstMultiplier it turns memory boost off.
	MemoryBoostMultiplier int64 `json:"memory_boost_multiplier"`
	// CPUBoostMultiplier times the CPU reservation is the boost ceiling a
	// running container's CPU quota may be raised to while the host CPU is
	// idle. At or below MaxCPUBurstMultiplier it turns CPU boost off.
	CPUBoostMultiplier int64 `json:"cpu_boost_multiplier"`
	// BoostMinFreeMemoryBytes is the free part of the user memory pool that
	// must remain after a boost is granted. Below it boosted limits shrink.
	BoostMinFreeMemoryBytes int64 `json:"boost_min_free_memory_bytes"`

	// Container lifecycle and disk.
	ContainerTTLHours    int64  `json:"container_ttl_hours"`
	ContainerStopTimeout int    `json:"container_stop_timeout"`
	ContainerPidsLimit   int64  `json:"container_pids_limit"`
	ContainerDiskQuota   string `json:"container_disk_quota"`
	MaxLogSize           string `json:"max_log_size"`
	// DefaultVolumeSizeBytes is the size of a volume created without one,
	// including every volume a Compose deployment creates.
	DefaultVolumeSizeBytes int64 `json:"default_volume_size_bytes"`
	// VolumeSizeEnforcement gives each new volume's size to Docker so the
	// kernel refuses writes past it. Turning it on requires a host whose
	// Docker data directory supports project quotas.
	VolumeSizeEnforcement bool `json:"volume_size_enforcement"`

	// What one user may hold. DefaultUserQuotaCPU, DefaultUserQuotaRAMMB and
	// DefaultUserQuotaDiskMB are the quota of every user without a custom
	// quota, in cores and MB. They are read at use, so a change applies at
	// each such user's next admission.
	DefaultUserQuotaCPU            float64 `json:"default_user_quota_cpu"`
	DefaultUserQuotaRAMMB          int64   `json:"default_user_quota_ram_mb"`
	DefaultUserQuotaDiskMB         int64   `json:"default_user_quota_disk_mb"`
	MaxContainersPerUser           int     `json:"max_containers_per_user"`
	MaxVolumesPerUser              int     `json:"max_volumes_per_user"`
	MaxQueuedBuildsPerUser         int     `json:"max_queued_builds_per_user"`
	MaxQueuedComposeDeploysPerUser int     `json:"max_queued_compose_deploys_per_user"`
	MaxStagedSourceBytesPerUser    int64   `json:"max_staged_source_bytes_per_user"`

	// Image builds. One build slot holds BuildMemoryBytes, BuildCPUMillicores
	// and BuildMaxDiskBytes; Builder stops a build that writes more disk.
	ImageBuildsEnabled   bool  `json:"image_builds_enabled"`
	MaxConcurrentBuilds  int   `json:"max_concurrent_builds"`
	BuildMemoryBytes     int64 `json:"build_memory_bytes"`
	BuildCPUMillicores   int64 `json:"build_cpu_millicores"`
	BuildMaxDiskBytes    int64 `json:"build_max_disk_bytes"`
	MaxBuildTimeMinutes  int64 `json:"max_build_time_minutes"`
	MaxBuildLogSizeBytes int64 `json:"max_build_log_size_bytes"`
	MaxArchiveSizeBytes  int64 `json:"max_archive_size_bytes"`
	MaxUnpackedSizeBytes int64 `json:"max_unpacked_size_bytes"`

	// Git sources.
	GitSourcesEnabled      bool     `json:"git_sources_enabled"`
	GitAllowedHosts        []string `json:"git_allowed_hosts"`
	GitCloneTimeoutSeconds int64    `json:"git_clone_timeout_seconds"`
	GitMaxRepositoryBytes  int64    `json:"git_max_repository_bytes"`

	// Compose deployments.
	ComposeUploadMaxBytes               int64 `json:"compose_upload_max_bytes"`
	ComposePipelineTimeoutMinutes       int64 `json:"compose_pipeline_timeout_minutes"`
	ComposeDependencyWaitTimeoutMinutes int64 `json:"compose_dependency_wait_timeout_minutes"`

	// Public subdomains users may not claim.
	ReservedDomainPrefixes      []string `json:"reserved_domain_prefixes"`
	BlockedDomainPrefixPatterns []string `json:"blocked_domain_prefix_patterns"`

	// Browser terminal.
	TelemetryAllowedExecCommands        []string `json:"telemetry_allowed_exec_commands"`
	TelemetryTerminalIdleTimeoutSeconds int64    `json:"telemetry_terminal_idle_timeout_seconds"`
	TelemetryTerminalMaxDurationSeconds int64    `json:"telemetry_terminal_max_duration_seconds"`

	Fixed `json:"-"`
}

// BuildCPUQuota is the CPU quota of a build container over BuildCPUPeriod.
func (c SystemConfig) BuildCPUQuota() int64 {
	return c.BuildCPUMillicores * c.BuildCPUPeriod / 1000
}

// Fixed is the part of SystemConfig that is set when Core starts: deployment
// names and addresses, and the tuning of workers, queues and Docker limits.
// The values come from the environment or their defaults, are not runtime
// settings, and are neither stored in the config file nor exposed through
// /admin/config. Changing one takes a Core restart.
type Fixed struct {
	// Deployment names and addresses.
	BaseDomain            string
	RegistryAPIURL        string
	RegistryPublicURL     string
	RegistryContainerName string
	ProxyNetworkName      string
	BuildNetworkName      string
	KanikoImage           string

	// Docker limits of user containers beyond the runtime settings.
	ContainerCPUPeriod            int64
	DefaultCPUShares              int64
	ContainerMemorySwapMultiplier float64
	MaxLogFiles                   string
	// MaxQueuedContainerCreatesPerUser bounds one user's container creates
	// waiting or running at once; zero means no bound.
	MaxQueuedContainerCreatesPerUser int

	// Docker limits of build containers beyond the runtime settings.
	BuildCPUPeriod                 int64
	BuildMemorySwapMultiplier      float64
	BuildPidsLimit                 int64
	BuildCancelPollIntervalSeconds int64

	// Lifecycle queue workers. The ContainerCreate settings also drive
	// volume and image operations.
	ContainerCreateWorkerCount           int
	ContainerCreateMaxAttempts           int
	ContainerCreateTimeoutMinutes        int64
	ContainerCreateOutboxIntervalSeconds int64
	ContainerCreateOutboxBatchSize       int
	BuildOutboxIntervalSeconds           int64
	BuildOutboxBatchSize                 int
	ComposeDeployWorkerCount             int
	ComposeDeployMaxAttempts             int
	ComposeOutboxIntervalSeconds         int64
	ComposeOutboxBatchSize               int
	ComposeCoordinatorIntervalSeconds    int64

	// Reconciliation, cleanup and measurement workers.
	TTLWorkerIntervalSeconds   int64
	EventSyncIntervalSeconds   int64
	EventReconnectDelaySeconds int64
	GCWorkerIntervalMinutes    int64
	// StaleBuildTimeoutMinutes is the least time a build may run before the
	// GC pass fails it. The pass never uses less than the build time budget
	// plus headroom, whatever this says.
	StaleBuildTimeoutMinutes         int64
	RegistryCleanupIntervalSeconds   int64
	RegistryCleanupJobTimeoutSeconds int64
	// RegistryGCMaxDurationSeconds is the Registry garbage collection lease:
	// no build is claimed until collection finishes or the lease expires.
	RegistryGCMaxDurationSeconds        int64
	VolumeUsageIntervalSeconds          int64
	VolumeUsageCycleBudgetSeconds       int64
	VolumeUsageScanEntryBudget          int
	MemoryPressureIntervalSeconds       int64
	ReportsUsageSnapshotIntervalSeconds int64

	// Container stream limits. The global limits are bootstrap settings.
	TelemetryMaxLogTailLines            int
	TelemetryMaxLogStreamsPerUser       int
	TelemetryMaxTerminalSessionsPerUser int
	TelemetryMaxCommandArgs             int
	TelemetryMaxCommandArgBytes         int
	TelemetryWSReadLimitBytes           int64
}
