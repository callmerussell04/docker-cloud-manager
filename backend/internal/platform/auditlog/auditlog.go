package auditlog

const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"

	ActorScopeUser    = "user"
	ActorScopeAdmin   = "admin"
	ActorScopeService = "service"
	ActorScopeUnknown = "unknown"

	ResourceContainer = "container"
	ResourceVolume    = "volume"
	ResourceImage     = "image"
	ResourceBuild     = "build"
	ResourceProject   = "project"
	ResourceUser      = "user"
	ResourceAuth      = "auth"
	ResourceTelemetry = "telemetry"
	ResourceSystem    = "system"

	ActionContainerCreate = "container.create"
	ActionContainerStart  = "container.start"
	ActionContainerStop   = "container.stop"
	ActionContainerDelete = "container.delete"
	ActionContainerExpose = "container.expose"

	ActionVolumeCreate = "volume.create"
	ActionVolumeDelete = "volume.delete"

	ActionImageDelete = "image.delete"

	ActionBuildCreateArchive = "build.create_archive"
	ActionBuildCreateGit     = "build.create_git"
	ActionBuildCancel        = "build.cancel"
	ActionBuildDelete        = "build.delete"

	ActionComposeUploadDeploy = "compose.upload_deploy"
	ActionComposeGitDeploy    = "compose.git_deploy"
	ActionProjectStart        = "project.start"
	ActionProjectStop         = "project.stop"
	ActionProjectCancel       = "project.cancel"
	ActionProjectDelete       = "project.delete"

	ActionAuthRegister     = "auth.register"
	ActionAuthLogin        = "auth.login"
	ActionAuthOIDCCallback = "auth.oidc_callback"

	ActionUserAdminCreate     = "user.admin_create"
	ActionUserAdminUpdate     = "user.admin_update"
	ActionUserAdminDeactivate = "user.admin_deactivate"
	ActionUserAdminReactivate = "user.admin_reactivate"
	ActionUserQuotaSet        = "user.quota_set"
	ActionUserQuotaReset      = "user.quota_reset"

	ActionTelemetryLogsTicket     = "telemetry.logs_ticket"
	ActionTelemetryTerminalTicket = "telemetry.terminal_ticket"

	DetailSourceType           = "source_type"
	DetailContainerID          = "container_id"
	DetailContainerName        = "container_name"
	DetailProjectID            = "project_id"
	DetailProjectName          = "project_name"
	DetailComposeService       = "compose_service"
	DetailDomainPrefix         = "domain_prefix"
	DetailFullDomain           = "full_domain"
	DetailInternalPort         = "internal_port"
	DetailPreviousDomainPrefix = "previous_domain_prefix"
	DetailPreviousInternalPort = "previous_internal_port"
	DetailProvider             = "provider"
	DetailAdmin                = "admin"
	DetailQuotaCPU             = "quota_cpu"
	DetailQuotaRAMMB           = "quota_ram_mb"
	DetailQuotaDiskMB          = "quota_disk_mb"
	DetailStoppedContainers    = "stopped_containers"
	DetailCanceledBuilds       = "canceled_builds"
	DetailCanceledDeployments  = "canceled_deployments"
	DetailRemainingWorkloads   = "remaining_workloads"
)
