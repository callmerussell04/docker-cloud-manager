import type { TranslationKey } from '@/shared/i18n';
import type { SystemConfigForm } from '../schemas';

export type ConfigFieldName = keyof SystemConfigForm & string;
export type ConfigFieldType = 'text' | 'number' | 'float' | 'boolean' | 'array' | 'bytes';
export type ConfigFieldConfig = {
  name: ConfigFieldName;
  labelKey: TranslationKey;
  hintKey: TranslationKey;
  type?: ConfigFieldType;
  unitKey?: TranslationKey;
  placeholderKey?: TranslationKey;
};

// The runtime settings form, one card per section.
export const sections: Array<{
  titleKey: TranslationKey;
  fields: ConfigFieldConfig[];
}> = [
  {
    titleKey: 'admin.settings.section.runtime',
    fields: [
      { name: 'base_domain', labelKey: 'admin.settings.field.base_domain.label', hintKey: 'admin.settings.field.base_domain.hint', placeholderKey: 'admin.settings.placeholder.domain' },
      { name: 'default_memory_reservation_bytes', labelKey: 'admin.settings.field.default_memory_reservation_bytes.label', hintKey: 'admin.settings.field.default_memory_reservation_bytes.hint', type: 'bytes' },
      { name: 'reserved_system_memory_bytes', labelKey: 'admin.settings.field.reserved_system_memory_bytes.label', hintKey: 'admin.settings.field.reserved_system_memory_bytes.hint', type: 'bytes' },
      { name: 'overcommit_factor', labelKey: 'admin.settings.field.overcommit_factor.label', hintKey: 'admin.settings.field.overcommit_factor.hint', type: 'float' },
      { name: 'max_burst_multiplier', labelKey: 'admin.settings.field.max_burst_multiplier.label', hintKey: 'admin.settings.field.max_burst_multiplier.hint', type: 'number' },
      { name: 'default_cpu_reservation_millicores', labelKey: 'admin.settings.field.default_cpu_reservation_millicores.label', hintKey: 'admin.settings.field.default_cpu_reservation_millicores.hint', type: 'number' },
      { name: 'reserved_system_cpu_millicores', labelKey: 'admin.settings.field.reserved_system_cpu_millicores.label', hintKey: 'admin.settings.field.reserved_system_cpu_millicores.hint', type: 'number' },
      { name: 'cpu_overcommit_factor', labelKey: 'admin.settings.field.cpu_overcommit_factor.label', hintKey: 'admin.settings.field.cpu_overcommit_factor.hint', type: 'float' },
      { name: 'max_cpu_burst_multiplier', labelKey: 'admin.settings.field.max_cpu_burst_multiplier.label', hintKey: 'admin.settings.field.max_cpu_burst_multiplier.hint', type: 'number' },
      { name: 'container_cpu_period', labelKey: 'admin.settings.field.container_cpu_period.label', hintKey: 'admin.settings.field.container_cpu_period.hint', type: 'number' },
      { name: 'default_cpu_shares', labelKey: 'admin.settings.field.default_cpu_shares.label', hintKey: 'admin.settings.field.default_cpu_shares.hint', type: 'number' },
      { name: 'container_stop_timeout', labelKey: 'admin.settings.field.container_stop_timeout.label', hintKey: 'admin.settings.field.container_stop_timeout.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'container_ttl_hours', labelKey: 'admin.settings.field.container_ttl_hours.label', hintKey: 'admin.settings.field.container_ttl_hours.hint', type: 'number', unitKey: 'admin.settings.unit.hours' },
      { name: 'container_pids_limit', labelKey: 'admin.settings.field.container_pids_limit.label', hintKey: 'admin.settings.field.container_pids_limit.hint', type: 'number' },
      { name: 'container_memory_swap_multiplier', labelKey: 'admin.settings.field.container_memory_swap_multiplier.label', hintKey: 'admin.settings.field.container_memory_swap_multiplier.hint', type: 'float' },
      { name: 'max_volumes_per_user', labelKey: 'admin.settings.field.max_volumes_per_user.label', hintKey: 'admin.settings.field.max_volumes_per_user.hint', type: 'number' },
      { name: 'default_volume_size_bytes', labelKey: 'admin.settings.field.default_volume_size_bytes.label', hintKey: 'admin.settings.field.default_volume_size_bytes.hint', type: 'bytes' },
      { name: 'volume_size_enforcement', labelKey: 'admin.settings.field.volume_size_enforcement.label', hintKey: 'admin.settings.field.volume_size_enforcement.hint', type: 'boolean' },
      { name: 'default_user_quota_cpu', labelKey: 'admin.settings.field.default_user_quota_cpu.label', hintKey: 'admin.settings.field.default_user_quota_cpu.hint', type: 'float', unitKey: 'admin.settings.unit.cores' },
      { name: 'default_user_quota_ram_mb', labelKey: 'admin.settings.field.default_user_quota_ram_mb.label', hintKey: 'admin.settings.field.default_user_quota_ram_mb.hint', type: 'number', unitKey: 'admin.settings.unit.mb' },
      { name: 'default_user_quota_disk_mb', labelKey: 'admin.settings.field.default_user_quota_disk_mb.label', hintKey: 'admin.settings.field.default_user_quota_disk_mb.hint', type: 'number', unitKey: 'admin.settings.unit.mb' },
      { name: 'max_containers_per_user', labelKey: 'admin.settings.field.max_containers_per_user.label', hintKey: 'admin.settings.field.max_containers_per_user.hint', type: 'number' },
      { name: 'container_disk_quota', labelKey: 'admin.settings.field.container_disk_quota.label', hintKey: 'admin.settings.field.container_disk_quota.hint', placeholderKey: 'admin.settings.placeholder.dockerSize' },
      { name: 'max_log_size', labelKey: 'admin.settings.field.max_log_size.label', hintKey: 'admin.settings.field.max_log_size.hint', placeholderKey: 'admin.settings.placeholder.logSize' },
      { name: 'max_log_files', labelKey: 'admin.settings.field.max_log_files.label', hintKey: 'admin.settings.field.max_log_files.hint', placeholderKey: 'admin.settings.placeholder.logFiles' },
    ],
  },
  {
    titleKey: 'admin.settings.section.registry',
    fields: [
      { name: 'reserved_domain_prefixes', labelKey: 'admin.settings.field.reserved_domain_prefixes.label', hintKey: 'admin.settings.field.reserved_domain_prefixes.hint', type: 'array', placeholderKey: 'admin.settings.placeholder.addPrefix' },
      { name: 'blocked_domain_prefix_patterns', labelKey: 'admin.settings.field.blocked_domain_prefix_patterns.label', hintKey: 'admin.settings.field.blocked_domain_prefix_patterns.hint', type: 'array', placeholderKey: 'admin.settings.placeholder.addPrefixPattern' },
    ],
  },
  {
    titleKey: 'admin.settings.section.builds',
    fields: [
      { name: 'image_builds_enabled', labelKey: 'admin.settings.field.image_builds_enabled.label', hintKey: 'admin.settings.field.image_builds_enabled.hint', type: 'boolean' },
      { name: 'build_memory_bytes', labelKey: 'admin.settings.field.build_memory_bytes.label', hintKey: 'admin.settings.field.build_memory_bytes.hint', type: 'bytes' },
      { name: 'build_max_disk_bytes', labelKey: 'admin.settings.field.build_max_disk_bytes.label', hintKey: 'admin.settings.field.build_max_disk_bytes.hint', type: 'bytes' },
      { name: 'build_cpu_quota', labelKey: 'admin.settings.field.build_cpu_quota.label', hintKey: 'admin.settings.field.build_cpu_quota.hint', type: 'number' },
      { name: 'build_cpu_period', labelKey: 'admin.settings.field.build_cpu_period.label', hintKey: 'admin.settings.field.build_cpu_period.hint', type: 'number' },
      { name: 'build_memory_swap_multiplier', labelKey: 'admin.settings.field.build_memory_swap_multiplier.label', hintKey: 'admin.settings.field.build_memory_swap_multiplier.hint', type: 'float' },
      { name: 'build_pids_limit', labelKey: 'admin.settings.field.build_pids_limit.label', hintKey: 'admin.settings.field.build_pids_limit.hint', type: 'number' },
      { name: 'kaniko_image', labelKey: 'admin.settings.field.kaniko_image.label', hintKey: 'admin.settings.field.kaniko_image.hint' },
      { name: 'max_build_time_minutes', labelKey: 'admin.settings.field.max_build_time_minutes.label', hintKey: 'admin.settings.field.max_build_time_minutes.hint', type: 'number', unitKey: 'admin.settings.unit.minutes' },
      { name: 'max_concurrent_builds', labelKey: 'admin.settings.field.max_concurrent_builds.label', hintKey: 'admin.settings.field.max_concurrent_builds.hint', type: 'number' },
      { name: 'max_upload_size_bytes', labelKey: 'admin.settings.field.max_upload_size_bytes.label', hintKey: 'admin.settings.field.max_upload_size_bytes.hint', type: 'bytes' },
      { name: 'max_archive_size_bytes', labelKey: 'admin.settings.field.max_archive_size_bytes.label', hintKey: 'admin.settings.field.max_archive_size_bytes.hint', type: 'bytes' },
      { name: 'max_unpacked_size_bytes', labelKey: 'admin.settings.field.max_unpacked_size_bytes.label', hintKey: 'admin.settings.field.max_unpacked_size_bytes.hint', type: 'bytes' },
      { name: 'max_build_log_size_bytes', labelKey: 'admin.settings.field.max_build_log_size_bytes.label', hintKey: 'admin.settings.field.max_build_log_size_bytes.hint', type: 'bytes' },
      { name: 'build_cancel_poll_interval_seconds', labelKey: 'admin.settings.field.build_cancel_poll_interval_seconds.label', hintKey: 'admin.settings.field.build_cancel_poll_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
    ],
  },
  {
    titleKey: 'admin.settings.section.compose',
    fields: [
      { name: 'compose_upload_max_bytes', labelKey: 'admin.settings.field.compose_upload_max_bytes.label', hintKey: 'admin.settings.field.compose_upload_max_bytes.hint', type: 'bytes' },
      { name: 'compose_pipeline_timeout_minutes', labelKey: 'admin.settings.field.compose_pipeline_timeout_minutes.label', hintKey: 'admin.settings.field.compose_pipeline_timeout_minutes.hint', type: 'number', unitKey: 'admin.settings.unit.minutes' },
      { name: 'compose_coordinator_interval_seconds', labelKey: 'admin.settings.field.compose_coordinator_interval_seconds.label', hintKey: 'admin.settings.field.compose_coordinator_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'compose_build_poll_interval_seconds', labelKey: 'admin.settings.field.compose_build_poll_interval_seconds.label', hintKey: 'admin.settings.field.compose_build_poll_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'compose_dependency_wait_timeout_minutes', labelKey: 'admin.settings.field.compose_dependency_wait_timeout_minutes.label', hintKey: 'admin.settings.field.compose_dependency_wait_timeout_minutes.hint', type: 'number', unitKey: 'admin.settings.unit.minutes' },
      { name: 'compose_deploy_worker_count', labelKey: 'admin.settings.field.compose_deploy_worker_count.label', hintKey: 'admin.settings.field.compose_deploy_worker_count.hint', type: 'number' },
      { name: 'compose_outbox_interval_seconds', labelKey: 'admin.settings.field.compose_outbox_interval_seconds.label', hintKey: 'admin.settings.field.compose_outbox_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'compose_outbox_batch_size', labelKey: 'admin.settings.field.compose_outbox_batch_size.label', hintKey: 'admin.settings.field.compose_outbox_batch_size.hint', type: 'number' },
      { name: 'compose_deploy_max_attempts', labelKey: 'admin.settings.field.compose_deploy_max_attempts.label', hintKey: 'admin.settings.field.compose_deploy_max_attempts.hint', type: 'number' },
      { name: 'max_queued_compose_deploys_per_user', labelKey: 'admin.settings.field.max_queued_compose_deploys_per_user.label', hintKey: 'admin.settings.field.max_queued_compose_deploys_per_user.hint', type: 'number' },
    ],
  },
  {
    titleKey: 'admin.settings.section.git',
    fields: [
      { name: 'git_sources_enabled', labelKey: 'admin.settings.field.git_sources_enabled.label', hintKey: 'admin.settings.field.git_sources_enabled.hint', type: 'boolean' },
      { name: 'git_allowed_hosts', labelKey: 'admin.settings.field.git_allowed_hosts.label', hintKey: 'admin.settings.field.git_allowed_hosts.hint', type: 'array', placeholderKey: 'admin.settings.placeholder.addHost' },
      { name: 'git_clone_timeout_seconds', labelKey: 'admin.settings.field.git_clone_timeout_seconds.label', hintKey: 'admin.settings.field.git_clone_timeout_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'git_max_repository_bytes', labelKey: 'admin.settings.field.git_max_repository_bytes.label', hintKey: 'admin.settings.field.git_max_repository_bytes.hint', type: 'bytes' },
    ],
  },
  {
    titleKey: 'admin.settings.section.workers',
    fields: [
      { name: 'ttl_worker_interval_seconds', labelKey: 'admin.settings.field.ttl_worker_interval_seconds.label', hintKey: 'admin.settings.field.ttl_worker_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'gc_worker_interval_minutes', labelKey: 'admin.settings.field.gc_worker_interval_minutes.label', hintKey: 'admin.settings.field.gc_worker_interval_minutes.hint', type: 'number', unitKey: 'admin.settings.unit.minutes' },
      { name: 'stale_build_timeout_minutes', labelKey: 'admin.settings.field.stale_build_timeout_minutes.label', hintKey: 'admin.settings.field.stale_build_timeout_minutes.hint', type: 'number', unitKey: 'admin.settings.unit.minutes' },
      { name: 'event_sync_interval_seconds', labelKey: 'admin.settings.field.event_sync_interval_seconds.label', hintKey: 'admin.settings.field.event_sync_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'event_reconnect_delay_seconds', labelKey: 'admin.settings.field.event_reconnect_delay_seconds.label', hintKey: 'admin.settings.field.event_reconnect_delay_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'volume_usage_interval_seconds', labelKey: 'admin.settings.field.volume_usage_interval_seconds.label', hintKey: 'admin.settings.field.volume_usage_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'volume_usage_cycle_budget_seconds', labelKey: 'admin.settings.field.volume_usage_cycle_budget_seconds.label', hintKey: 'admin.settings.field.volume_usage_cycle_budget_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'volume_usage_scan_entry_budget', labelKey: 'admin.settings.field.volume_usage_scan_entry_budget.label', hintKey: 'admin.settings.field.volume_usage_scan_entry_budget.hint', type: 'number' },
      { name: 'registry_cleanup_interval_seconds', labelKey: 'admin.settings.field.registry_cleanup_interval_seconds.label', hintKey: 'admin.settings.field.registry_cleanup_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'registry_cleanup_job_timeout_seconds', labelKey: 'admin.settings.field.registry_cleanup_job_timeout_seconds.label', hintKey: 'admin.settings.field.registry_cleanup_job_timeout_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'registry_gc_max_duration_seconds', labelKey: 'admin.settings.field.registry_gc_max_duration_seconds.label', hintKey: 'admin.settings.field.registry_gc_max_duration_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'build_outbox_interval_seconds', labelKey: 'admin.settings.field.build_outbox_interval_seconds.label', hintKey: 'admin.settings.field.build_outbox_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'build_outbox_batch_size', labelKey: 'admin.settings.field.build_outbox_batch_size.label', hintKey: 'admin.settings.field.build_outbox_batch_size.hint', type: 'number' },
      { name: 'container_create_worker_count', labelKey: 'admin.settings.field.container_create_worker_count.label', hintKey: 'admin.settings.field.container_create_worker_count.hint', type: 'number' },
      { name: 'container_create_max_attempts', labelKey: 'admin.settings.field.container_create_max_attempts.label', hintKey: 'admin.settings.field.container_create_max_attempts.hint', type: 'number' },
      { name: 'container_create_timeout_minutes', labelKey: 'admin.settings.field.container_create_timeout_minutes.label', hintKey: 'admin.settings.field.container_create_timeout_minutes.hint', type: 'number', unitKey: 'admin.settings.unit.minutes' },
      { name: 'container_create_outbox_interval_seconds', labelKey: 'admin.settings.field.container_create_outbox_interval_seconds.label', hintKey: 'admin.settings.field.container_create_outbox_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'container_create_outbox_batch_size', labelKey: 'admin.settings.field.container_create_outbox_batch_size.label', hintKey: 'admin.settings.field.container_create_outbox_batch_size.hint', type: 'number' },
      { name: 'reports_usage_snapshot_interval_seconds', labelKey: 'admin.settings.field.reports_usage_snapshot_interval_seconds.label', hintKey: 'admin.settings.field.reports_usage_snapshot_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
    ],
  },
  {
    titleKey: 'admin.settings.section.safety',
    fields: [
      { name: 'max_queued_container_creates_per_user', labelKey: 'admin.settings.field.max_queued_container_creates_per_user.label', hintKey: 'admin.settings.field.max_queued_container_creates_per_user.hint', type: 'number' },
      { name: 'max_queued_builds_per_user', labelKey: 'admin.settings.field.max_queued_builds_per_user.label', hintKey: 'admin.settings.field.max_queued_builds_per_user.hint', type: 'number' },
      { name: 'max_staged_source_bytes_per_user', labelKey: 'admin.settings.field.max_staged_source_bytes_per_user.label', hintKey: 'admin.settings.field.max_staged_source_bytes_per_user.hint', type: 'bytes' },
      { name: 'host_min_free_disk_bytes', labelKey: 'admin.settings.field.host_min_free_disk_bytes.label', hintKey: 'admin.settings.field.host_min_free_disk_bytes.hint', type: 'bytes' },
      { name: 'memory_pressure_min_free_bytes', labelKey: 'admin.settings.field.memory_pressure_min_free_bytes.label', hintKey: 'admin.settings.field.memory_pressure_min_free_bytes.hint', type: 'bytes' },
      { name: 'memory_pressure_interval_seconds', labelKey: 'admin.settings.field.memory_pressure_interval_seconds.label', hintKey: 'admin.settings.field.memory_pressure_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'memory_boost_multiplier', labelKey: 'admin.settings.field.memory_boost_multiplier.label', hintKey: 'admin.settings.field.memory_boost_multiplier.hint', type: 'number' },
      { name: 'cpu_boost_multiplier', labelKey: 'admin.settings.field.cpu_boost_multiplier.label', hintKey: 'admin.settings.field.cpu_boost_multiplier.hint', type: 'number' },
      { name: 'boost_min_free_memory_bytes', labelKey: 'admin.settings.field.boost_min_free_memory_bytes.label', hintKey: 'admin.settings.field.boost_min_free_memory_bytes.hint', type: 'bytes' },
    ],
  },
  {
    titleKey: 'admin.settings.section.telemetry',
    fields: [
      { name: 'telemetry_max_log_tail_lines', labelKey: 'admin.settings.field.telemetry_max_log_tail_lines.label', hintKey: 'admin.settings.field.telemetry_max_log_tail_lines.hint', type: 'number', unitKey: 'admin.settings.unit.lines' },
      { name: 'telemetry_max_log_streams_per_user', labelKey: 'admin.settings.field.telemetry_max_log_streams_per_user.label', hintKey: 'admin.settings.field.telemetry_max_log_streams_per_user.hint', type: 'number' },
      { name: 'telemetry_max_terminal_sessions_per_user', labelKey: 'admin.settings.field.telemetry_max_terminal_sessions_per_user.label', hintKey: 'admin.settings.field.telemetry_max_terminal_sessions_per_user.hint', type: 'number' },
      { name: 'telemetry_terminal_idle_timeout_seconds', labelKey: 'admin.settings.field.telemetry_terminal_idle_timeout_seconds.label', hintKey: 'admin.settings.field.telemetry_terminal_idle_timeout_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'telemetry_terminal_max_duration_seconds', labelKey: 'admin.settings.field.telemetry_terminal_max_duration_seconds.label', hintKey: 'admin.settings.field.telemetry_terminal_max_duration_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'telemetry_allowed_exec_commands', labelKey: 'admin.settings.field.telemetry_allowed_exec_commands.label', hintKey: 'admin.settings.field.telemetry_allowed_exec_commands.hint', type: 'array', placeholderKey: 'admin.settings.placeholder.addCommand' },
      { name: 'telemetry_max_command_args', labelKey: 'admin.settings.field.telemetry_max_command_args.label', hintKey: 'admin.settings.field.telemetry_max_command_args.hint', type: 'number' },
      { name: 'telemetry_max_command_arg_bytes', labelKey: 'admin.settings.field.telemetry_max_command_arg_bytes.label', hintKey: 'admin.settings.field.telemetry_max_command_arg_bytes.hint', type: 'bytes' },
      { name: 'telemetry_ws_read_limit_bytes', labelKey: 'admin.settings.field.telemetry_ws_read_limit_bytes.label', hintKey: 'admin.settings.field.telemetry_ws_read_limit_bytes.hint', type: 'bytes' },
    ],
  },
];

export const byteUnits = [
  { label: 'Bytes', multiplier: 1 },
  { label: 'KB', multiplier: 1024 },
  { label: 'MB', multiplier: 1024 ** 2 },
  { label: 'GB', multiplier: 1024 ** 3 },
] as const;

export type ByteUnit = typeof byteUnits[number]['label'];

export function preferredByteUnit(bytes: number): ByteUnit {
  if (bytes >= 1024 ** 3 && bytes % (1024 ** 3) === 0) return 'GB';
  if (bytes >= 1024 ** 2 && bytes % (1024 ** 2) === 0) return 'MB';
  if (bytes >= 1024 && bytes % 1024 === 0) return 'KB';
  return 'Bytes';
}

export function numericInputValue(value: unknown) {
  if (value === '') return undefined;
  const numberValue = Number(value);
  return Number.isFinite(numberValue) ? numberValue : undefined;
}

export function formatNumberInput(value: number) {
  return Number.isFinite(value) ? String(value) : '';
}

export function getErrorMessage(error: unknown) {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string') {
    return error.message;
  }
  return undefined;
}
