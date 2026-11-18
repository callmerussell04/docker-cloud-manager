import type { SettingName } from './model';

/**
 * How a setting is entered. `bytes` and `cores` are shown in friendlier units
 * than the contract's bytes and millicores; `size` is a Docker size string
 * such as 10m or 1G.
 */
export type SettingKind = 'integer' | 'decimal' | 'bytes' | 'cores' | 'size' | 'toggle' | 'list';

/** The form of a list entry: a DNS-label prefix, a regular expression, a host name or a command path. */
export type ListItemKind = 'prefix' | 'pattern' | 'host' | 'command';

export type SettingUnit = 'seconds' | 'minutes' | 'hours' | 'mb' | 'cores' | 'times';

/** One runtime setting: how it is entered and, in the contract's units, what the server accepts. */
export interface SettingDef {
  name: SettingName;
  kind: SettingKind;
  min?: number;
  max?: number;
  /** The value must be above `min`, not equal to it. */
  aboveMin?: boolean;
  unit?: SettingUnit;
  /** A list that must keep at least one entry. */
  required?: boolean;
  /** What each entry of a list must look like; entries are also unique. */
  item?: ListItemKind;
  /** Settings whose own rule depends on this one, checked again when it changes. */
  affects?: SettingName[];
}

export type SectionId =
  | 'users' | 'memory' | 'cpu' | 'disk' | 'containers' | 'builds' | 'git' | 'compose' | 'domains' | 'terminal';

const MB = 1024 ** 2;
const maxMultiplier = 10;
const maxBoostMultiplier = 64;

/** The settings form: sections in page order, each with its settings in row order. */
export const sections: Array<{ id: SectionId; settings: SettingDef[] }> = [
  {
    id: 'users',
    settings: [
      { name: 'default_user_quota_cpu', kind: 'decimal', min: 0.001, max: 1_000_000, unit: 'cores' },
      { name: 'default_user_quota_ram_mb', kind: 'integer', min: 1, unit: 'mb' },
      { name: 'default_user_quota_disk_mb', kind: 'integer', min: 1, unit: 'mb' },
      { name: 'max_containers_per_user', kind: 'integer', min: 1 },
      { name: 'max_volumes_per_user', kind: 'integer', min: 1 },
      { name: 'max_queued_builds_per_user', kind: 'integer', min: 1 },
      { name: 'max_queued_compose_deploys_per_user', kind: 'integer', min: 1 },
      { name: 'max_staged_source_bytes_per_user', kind: 'bytes', min: 0 },
    ],
  },
  {
    id: 'memory',
    settings: [
      { name: 'default_memory_reservation_bytes', kind: 'bytes', min: 16 * MB },
      { name: 'max_burst_multiplier', kind: 'integer', min: 1, max: maxMultiplier, unit: 'times' },
      { name: 'memory_boost_multiplier', kind: 'integer', min: 1, max: maxBoostMultiplier, unit: 'times' },
      { name: 'boost_min_free_memory_bytes', kind: 'bytes', min: 0 },
      { name: 'memory_pressure_min_free_bytes', kind: 'bytes', min: 0, affects: ['boost_min_free_memory_bytes'] },
      { name: 'reserved_system_memory_bytes', kind: 'bytes', min: 0 },
      { name: 'overcommit_factor', kind: 'decimal', min: 0, aboveMin: true, max: maxMultiplier, unit: 'times' },
    ],
  },
  {
    id: 'cpu',
    settings: [
      { name: 'default_cpu_reservation_millicores', kind: 'cores', min: 10 },
      { name: 'max_cpu_burst_multiplier', kind: 'integer', min: 1, max: maxMultiplier, unit: 'times' },
      { name: 'cpu_boost_multiplier', kind: 'integer', min: 1, max: maxBoostMultiplier, unit: 'times' },
      { name: 'reserved_system_cpu_millicores', kind: 'cores', min: 0 },
      { name: 'cpu_overcommit_factor', kind: 'decimal', min: 0, aboveMin: true, max: maxMultiplier, unit: 'times' },
    ],
  },
  {
    id: 'disk',
    settings: [
      { name: 'container_disk_quota', kind: 'size' },
      { name: 'max_log_size', kind: 'size' },
      { name: 'default_volume_size_bytes', kind: 'bytes', min: MB },
      { name: 'volume_size_enforcement', kind: 'toggle' },
      { name: 'host_min_free_disk_bytes', kind: 'bytes', min: 0 },
    ],
  },
  {
    id: 'containers',
    settings: [
      { name: 'container_ttl_hours', kind: 'integer', min: 0, unit: 'hours' },
      { name: 'container_stop_timeout', kind: 'integer', min: 1, unit: 'seconds' },
      { name: 'container_pids_limit', kind: 'integer', min: 1 },
    ],
  },
  {
    id: 'builds',
    settings: [
      { name: 'image_builds_enabled', kind: 'toggle' },
      { name: 'max_concurrent_builds', kind: 'integer', min: 1 },
      { name: 'build_memory_bytes', kind: 'bytes', min: 1 },
      { name: 'build_cpu_millicores', kind: 'cores', min: 1 },
      { name: 'build_max_disk_bytes', kind: 'bytes', min: 64 * MB },
      { name: 'max_build_time_minutes', kind: 'integer', min: 1, unit: 'minutes' },
      { name: 'max_build_log_size_bytes', kind: 'bytes', min: 1 },
      { name: 'max_archive_size_bytes', kind: 'bytes', min: 1 },
      { name: 'max_unpacked_size_bytes', kind: 'bytes', min: 1 },
    ],
  },
  {
    id: 'git',
    settings: [
      { name: 'git_sources_enabled', kind: 'toggle', affects: ['git_allowed_hosts'] },
      { name: 'git_allowed_hosts', kind: 'list', item: 'host' },
      { name: 'git_clone_timeout_seconds', kind: 'integer', min: 1, unit: 'seconds' },
      { name: 'git_max_repository_bytes', kind: 'bytes', min: 1 },
    ],
  },
  {
    id: 'compose',
    settings: [
      { name: 'compose_upload_max_bytes', kind: 'bytes', min: 1 },
      { name: 'compose_pipeline_timeout_minutes', kind: 'integer', min: 1, unit: 'minutes' },
      { name: 'compose_dependency_wait_timeout_minutes', kind: 'integer', min: 1, unit: 'minutes' },
    ],
  },
  {
    id: 'domains',
    settings: [
      { name: 'reserved_domain_prefixes', kind: 'list', item: 'prefix' },
      { name: 'blocked_domain_prefix_patterns', kind: 'list', item: 'pattern' },
    ],
  },
  {
    id: 'terminal',
    settings: [
      { name: 'telemetry_allowed_exec_commands', kind: 'list', item: 'command', required: true },
      { name: 'telemetry_terminal_idle_timeout_seconds', kind: 'integer', min: 1, unit: 'seconds' },
      { name: 'telemetry_terminal_max_duration_seconds', kind: 'integer', min: 1, unit: 'seconds' },
    ],
  },
];

export const settingDefs = Object.fromEntries(
  sections.flatMap((section) => section.settings.map((setting) => [setting.name, setting])),
) as Record<SettingName, SettingDef>;

/** The section a setting is shown in. */
export const sectionOf = Object.fromEntries(
  sections.flatMap((section) => section.settings.map((setting) => [setting.name, section.id])),
) as Record<SettingName, SectionId>;
