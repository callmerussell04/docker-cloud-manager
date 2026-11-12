import { z } from 'zod';
import type { TFunction } from '@/lib/i18n';

export interface AdminUserAccount {
  user_id: string;
  username: string;
  email: string;
  role: 'admin' | 'user';
  status: 'active' | 'deactivated';
}

/** A user's effective quota from Core. quota_custom is false for the default quota. */
export interface UserQuota {
  quota_cpu: number;
  quota_ram_mb: number;
  quota_disk_mb: number;
  quota_custom: boolean;
}

export interface AdminUser extends AdminUserAccount, UserQuota {}

/** What deactivation did to the user's workloads. remaining counts those not yet stopped or canceled. */
export interface WorkloadSuspension {
  stopped_containers: number;
  canceled_builds: number;
  canceled_deployments: number;
  remaining: number;
}

export interface AdminUserDeactivation extends AdminUserAccount {
  workloads: WorkloadSuspension;
}

export interface SystemConfig {
  base_domain: string;
  default_memory_reservation_bytes: number;
  reserved_system_memory_bytes: number;
  overcommit_factor: number;
  max_burst_multiplier: number;
  default_cpu_reservation_millicores: number;
  reserved_system_cpu_millicores: number;
  cpu_overcommit_factor: number;
  max_cpu_burst_multiplier: number;
  container_cpu_period: number;
  default_cpu_shares: number;
  container_stop_timeout: number;
  max_log_size: string;
  max_log_files: string;
  container_disk_quota: string;
  reserved_domain_prefixes: string[];
  blocked_domain_prefix_patterns: string[];
  max_volumes_per_user: number;
  max_containers_per_user: number;
  container_ttl_hours: number;
  container_pids_limit: number;
  container_memory_swap_multiplier: number;
  image_builds_enabled: boolean;
  build_memory_bytes: number;
  build_cpu_quota: number;
  build_cpu_period: number;
  build_memory_swap_multiplier: number;
  build_pids_limit: number;
  kaniko_image: string;
  max_build_time_minutes: number;
  max_concurrent_builds: number;
  max_upload_size_bytes: number;
  max_archive_size_bytes: number;
  max_unpacked_size_bytes: number;
  max_build_log_size_bytes: number;
  build_cancel_poll_interval_seconds: number;
  ttl_worker_interval_seconds: number;
  gc_worker_interval_minutes: number;
  stale_build_timeout_minutes: number;
  event_sync_interval_seconds: number;
  event_reconnect_delay_seconds: number;
  volume_usage_interval_seconds: number;
  volume_usage_cycle_budget_seconds: number;
  volume_usage_scan_entry_budget: number;
  registry_cleanup_interval_seconds: number;
  registry_cleanup_job_timeout_seconds: number;
  build_outbox_interval_seconds: number;
  build_outbox_batch_size: number;
  reports_usage_snapshot_interval_seconds: number;
  container_create_worker_count: number;
  container_create_max_attempts: number;
  container_create_timeout_minutes: number;
  max_queued_container_creates_per_user: number;
  container_create_outbox_interval_seconds: number;
  container_create_outbox_batch_size: number;
  max_staged_source_bytes_per_user: number;
  max_queued_builds_per_user: number;
  compose_upload_max_bytes: number;
  compose_pipeline_timeout_minutes: number;
  compose_build_poll_interval_seconds: number;
  compose_dependency_wait_timeout_minutes: number;
  compose_deploy_worker_count: number;
  compose_outbox_interval_seconds: number;
  compose_outbox_batch_size: number;
  compose_deploy_max_attempts: number;
  max_queued_compose_deploys_per_user: number;
  compose_coordinator_interval_seconds: number;
  host_min_free_disk_bytes: number;
  memory_pressure_min_free_bytes: number;
  memory_pressure_interval_seconds: number;
  memory_boost_multiplier: number;
  cpu_boost_multiplier: number;
  boost_min_free_memory_bytes: number;
  default_volume_size_bytes: number;
  volume_size_enforcement: boolean;
  default_user_quota_cpu: number;
  default_user_quota_ram_mb: number;
  default_user_quota_disk_mb: number;
  git_sources_enabled: boolean;
  git_allowed_hosts: string[];
  git_clone_timeout_seconds: number;
  git_max_repository_bytes: number;
  telemetry_max_log_tail_lines: number;
  telemetry_max_log_streams_per_user: number;
  telemetry_max_terminal_sessions_per_user: number;
  telemetry_terminal_idle_timeout_seconds: number;
  telemetry_terminal_max_duration_seconds: number;
  telemetry_allowed_exec_commands: string[];
  telemetry_max_command_args: number;
  telemetry_max_command_arg_bytes: number;
  telemetry_ws_read_limit_bytes: number;
}

export type RootfsQuotaStatus = 'enforced' | 'not_enforced' | 'unknown';

export type VolumeSizeMode = 'measured' | 'enforced' | 'unavailable';

export interface SystemMonitoring {
  cpu_percent: number;
  memory_total_bytes: number;
  memory_used_bytes: number;
  memory_available_bytes: number;
  disk_total_bytes: number;
  disk_used_bytes: number;
  disk_free_bytes: number;
  dcm_reserved_memory_bytes: number;
  dcm_reserved_build_memory_bytes: number;
  dcm_borrowed_memory_bytes: number;
  dcm_disk_used_bytes: number;
  dcm_reserved_volume_bytes: number;
  host_min_free_disk_bytes: number;
  admission_status: string;
  admission_reasons: string[];
  rootfs_quota_status: RootfsQuotaStatus;
  volume_size_mode: VolumeSizeMode;
  containers_total: number;
  containers_running: number;
  containers_stopped: number;
  containers_error: number;
  containers_missing: number;
  volumes_total: number;
  images_total: number;
  builds_total: number;
  projects_total: number;
  observed_at: number;
}

const stringListSchema = z
  .array(z.string().trim())
  .nullish()
  .transform((items) => (items ?? []).filter(Boolean));

export const systemConfigSchema = z.object({
  base_domain: z.string().min(1),
  default_memory_reservation_bytes: z.number().min(1),
  reserved_system_memory_bytes: z.number().min(0),
  overcommit_factor: z.number().positive().max(10),
  max_burst_multiplier: z.number().min(1),
  default_cpu_reservation_millicores: z.number().min(1),
  reserved_system_cpu_millicores: z.number().min(0),
  cpu_overcommit_factor: z.number().positive().max(10),
  max_cpu_burst_multiplier: z.number().min(1),
  container_cpu_period: z.number().min(1),
  default_cpu_shares: z.number().min(2),
  container_stop_timeout: z.number().min(1),
  max_log_size: z.string().min(1),
  max_log_files: z.string().min(1),
  container_disk_quota: z.string().min(1),
  reserved_domain_prefixes: stringListSchema,
  blocked_domain_prefix_patterns: stringListSchema,
  max_volumes_per_user: z.number().min(1),
  max_containers_per_user: z.number().min(1),
  container_ttl_hours: z.number().min(0),
  container_pids_limit: z.number().min(1),
  container_memory_swap_multiplier: z.number().min(1),
  image_builds_enabled: z.boolean(),
  build_memory_bytes: z.number().min(1),
  build_cpu_quota: z.number().min(1),
  build_cpu_period: z.number().min(1),
  build_memory_swap_multiplier: z.number().min(1),
  build_pids_limit: z.number().min(1),
  kaniko_image: z.string().min(1),
  max_build_time_minutes: z.number().min(1),
  max_concurrent_builds: z.number().min(1),
  max_upload_size_bytes: z.number().min(1),
  max_archive_size_bytes: z.number().min(1),
  max_unpacked_size_bytes: z.number().min(1),
  max_build_log_size_bytes: z.number().min(1),
  build_cancel_poll_interval_seconds: z.number().min(1),
  ttl_worker_interval_seconds: z.number().min(1),
  gc_worker_interval_minutes: z.number().min(1),
  stale_build_timeout_minutes: z.number().min(1),
  event_sync_interval_seconds: z.number().min(1),
  event_reconnect_delay_seconds: z.number().min(1),
  volume_usage_interval_seconds: z.number().min(1),
  volume_usage_cycle_budget_seconds: z.number().min(1),
  volume_usage_scan_entry_budget: z.number().min(1).max(10000000),
  registry_cleanup_interval_seconds: z.number().min(1),
  registry_cleanup_job_timeout_seconds: z.number().min(1),
  build_outbox_interval_seconds: z.number().min(1),
  build_outbox_batch_size: z.number().min(1),
  reports_usage_snapshot_interval_seconds: z.number().min(60).max(86400),
  container_create_worker_count: z.number().min(1),
  container_create_max_attempts: z.number().min(1),
  container_create_timeout_minutes: z.number().min(1),
  max_queued_container_creates_per_user: z.number().min(0),
  container_create_outbox_interval_seconds: z.number().min(1),
  container_create_outbox_batch_size: z.number().min(1),
  max_staged_source_bytes_per_user: z.number().min(0),
  max_queued_builds_per_user: z.number().min(0),
  compose_upload_max_bytes: z.number().min(1),
  compose_pipeline_timeout_minutes: z.number().min(1),
  compose_build_poll_interval_seconds: z.number().min(1),
  compose_dependency_wait_timeout_minutes: z.number().min(1),
  compose_deploy_worker_count: z.number().min(1),
  compose_outbox_interval_seconds: z.number().min(1),
  compose_outbox_batch_size: z.number().min(1),
  compose_deploy_max_attempts: z.number().min(1),
  max_queued_compose_deploys_per_user: z.number().min(0),
  compose_coordinator_interval_seconds: z.number().min(1),
  host_min_free_disk_bytes: z.number().min(0),
  memory_pressure_min_free_bytes: z.number().min(0),
  memory_pressure_interval_seconds: z.number().min(1),
  memory_boost_multiplier: z.number().int().min(1).max(64),
  cpu_boost_multiplier: z.number().int().min(1).max(64),
  boost_min_free_memory_bytes: z.number().min(0),
  default_volume_size_bytes: z.number().min(1024 * 1024),
  volume_size_enforcement: z.boolean(),
  default_user_quota_cpu: z.number().min(0.001).max(1_000_000),
  default_user_quota_ram_mb: z.number().int().min(1),
  default_user_quota_disk_mb: z.number().int().min(1),
  git_sources_enabled: z.boolean(),
  git_allowed_hosts: stringListSchema,
  git_clone_timeout_seconds: z.number().min(1),
  git_max_repository_bytes: z.number().min(1),
  telemetry_max_log_tail_lines: z.number().min(1),
  telemetry_max_log_streams_per_user: z.number().min(1),
  telemetry_max_terminal_sessions_per_user: z.number().min(1),
  telemetry_terminal_idle_timeout_seconds: z.number().min(1),
  telemetry_terminal_max_duration_seconds: z.number().min(1),
  telemetry_allowed_exec_commands: stringListSchema,
  telemetry_max_command_args: z.number().min(0),
  telemetry_max_command_arg_bytes: z.number().min(1),
  telemetry_ws_read_limit_bytes: z.number().min(1),
});

export type SystemConfigForm = z.input<typeof systemConfigSchema>;

export const adminUserSchema = (t: TFunction) => z.object({
  username: z.string().min(1, t('validation.nameRequired')),
  email: z.string().email(t('validation.emailInvalid')),
  password: z.string().optional(),
  role: z.enum(['admin', 'user']),
  status: z.enum(['active', 'deactivated']),
});

export type AdminUserForm = z.input<ReturnType<typeof adminUserSchema>>;

export const userQuotaSchema = (t: TFunction) => z.object({
  quota_cpu: z.number({ error: t('validation.quotaPositive') }).min(0.001, t('validation.quotaPositive')).max(1_000_000, t('validation.quotaPositive')),
  quota_ram_mb: z.number({ error: t('validation.quotaPositive') }).int().min(1, t('validation.quotaPositive')),
  quota_disk_mb: z.number({ error: t('validation.quotaPositive') }).int().min(1, t('validation.quotaPositive')),
});

export type UserQuotaForm = z.input<ReturnType<typeof userQuotaSchema>>;
