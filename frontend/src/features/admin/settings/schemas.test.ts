import { describe, expect, it } from 'vitest';

import { sections, settingDefs } from './catalogue';
import type { SettingName, SystemConfig } from './model';
import { systemConfigSchema } from './schemas';

const MB = 1024 ** 2;
const GB = 1024 ** 3;

// The values a new installation starts with.
const defaults: SystemConfig = {
  default_user_quota_cpu: 1,
  default_user_quota_ram_mb: 2048,
  default_user_quota_disk_mb: 5120,
  max_containers_per_user: 10,
  max_volumes_per_user: 5,
  max_queued_builds_per_user: 10,
  max_queued_compose_deploys_per_user: 5,
  max_staged_source_bytes_per_user: GB,
  default_memory_reservation_bytes: 256 * MB,
  max_burst_multiplier: 4,
  memory_boost_multiplier: 8,
  boost_min_free_memory_bytes: 2 * GB,
  memory_pressure_min_free_bytes: 512 * MB,
  reserved_system_memory_bytes: 2 * GB,
  overcommit_factor: 1,
  default_cpu_reservation_millicores: 250,
  max_cpu_burst_multiplier: 4,
  cpu_boost_multiplier: 8,
  reserved_system_cpu_millicores: 500,
  cpu_overcommit_factor: 4,
  container_disk_quota: '1G',
  max_log_size: '10m',
  default_volume_size_bytes: 256 * MB,
  volume_size_enforcement: false,
  host_min_free_disk_bytes: GB,
  container_ttl_hours: 24,
  container_stop_timeout: 10,
  container_pids_limit: 256,
  image_builds_enabled: true,
  max_concurrent_builds: 2,
  build_memory_bytes: 512 * MB,
  build_cpu_millicores: 1000,
  build_max_disk_bytes: 10 * GB,
  max_build_time_minutes: 10,
  max_build_log_size_bytes: 5 * MB,
  max_archive_size_bytes: 50 * MB,
  max_unpacked_size_bytes: 500 * MB,
  git_sources_enabled: true,
  git_allowed_hosts: ['github.com', 'gitlab.com', 'bitbucket.org'],
  git_clone_timeout_seconds: 60,
  git_max_repository_bytes: 200 * MB,
  compose_upload_max_bytes: 100 * MB,
  compose_pipeline_timeout_minutes: 30,
  compose_dependency_wait_timeout_minutes: 5,
  reserved_domain_prefixes: ['api', 'admin'],
  blocked_domain_prefix_patterns: [],
  telemetry_allowed_exec_commands: ['/bin/sh', '/bin/bash'],
  telemetry_terminal_idle_timeout_seconds: 300,
  telemetry_terminal_max_duration_seconds: 3600,
};

function errorsOf(change: Partial<Record<SettingName, unknown>>) {
  const result = systemConfigSchema.safeParse({ ...defaults, ...change });
  if (result.success) return {};
  return Object.fromEntries(result.error.issues.map((issue) => [issue.path.join('.'), issue.message]));
}

describe('settings catalogue', () => {
  it('lists every runtime setting in exactly one section', () => {
    const listed = sections.flatMap((section) => section.settings.map((setting) => setting.name));
    expect([...listed].sort()).toEqual(Object.keys(defaults).sort());
    expect(Object.keys(settingDefs).sort()).toEqual(Object.keys(defaults).sort());
  });
});

describe('settings schema', () => {
  it('accepts the defaults and returns them unchanged', () => {
    expect(systemConfigSchema.parse(defaults)).toEqual(defaults);
  });

  it('accepts the values that turn a protection off', () => {
    expect(errorsOf({
      container_ttl_hours: 0,
      host_min_free_disk_bytes: 0,
      memory_pressure_min_free_bytes: 0,
      boost_min_free_memory_bytes: 0,
      reserved_system_memory_bytes: 0,
      reserved_system_cpu_millicores: 0,
      max_staged_source_bytes_per_user: 0,
      memory_boost_multiplier: 1,
      cpu_boost_multiplier: 1,
    })).toEqual({});
  });

  it.each<[SettingName, unknown, string]>([
    ['default_user_quota_cpu', 0, 'range'],
    ['default_user_quota_cpu', 1_000_001, 'range'],
    ['default_user_quota_ram_mb', 0, 'range'],
    ['default_user_quota_ram_mb', 1.5, 'integer'],
    ['max_containers_per_user', 0, 'range'],
    ['max_queued_builds_per_user', 0, 'range'],
    ['max_queued_compose_deploys_per_user', 0, 'range'],
    ['max_staged_source_bytes_per_user', -1, 'range'],
    ['default_memory_reservation_bytes', 0, 'range'],
    ['max_burst_multiplier', 0, 'range'],
    ['max_burst_multiplier', 11, 'range'],
    ['memory_boost_multiplier', 65, 'range'],
    ['overcommit_factor', 0, 'range'],
    ['overcommit_factor', 10.5, 'range'],
    ['default_cpu_reservation_millicores', 0, 'range'],
    ['max_cpu_burst_multiplier', 11, 'range'],
    ['cpu_boost_multiplier', 0, 'range'],
    ['cpu_overcommit_factor', 0, 'range'],
    ['container_disk_quota', '1', 'size'],
    ['container_disk_quota', '0G', 'size'],
    ['max_log_size', '10 mb', 'size'],
    ['default_volume_size_bytes', MB - 1, 'range'],
    ['container_ttl_hours', -1, 'range'],
    ['container_stop_timeout', 0, 'range'],
    ['container_pids_limit', 0, 'range'],
    ['max_concurrent_builds', 0, 'range'],
    ['build_cpu_millicores', 0, 'range'],
    ['build_max_disk_bytes', 64 * MB - 1, 'range'],
    ['max_build_time_minutes', 0, 'range'],
    ['max_archive_size_bytes', 0, 'range'],
    ['git_clone_timeout_seconds', 0, 'range'],
    ['compose_pipeline_timeout_minutes', 0, 'range'],
    ['telemetry_terminal_idle_timeout_seconds', 0, 'range'],
    ['telemetry_allowed_exec_commands', [], 'listRequired'],
    ['max_build_time_minutes', undefined, 'required'],
    ['build_memory_bytes', Number.NaN, 'required'],
  ])('rejects %s = %j as %s', (name, value, code) => {
    expect(errorsOf({ [name]: value })).toEqual({ [name]: code });
  });

  it('requires the boost floor to be at least the memory pressure floor', () => {
    expect(errorsOf({ memory_pressure_min_free_bytes: GB, boost_min_free_memory_bytes: GB - 1 }))
      .toEqual({ boost_min_free_memory_bytes: 'boostFloor' });
    expect(errorsOf({ memory_pressure_min_free_bytes: GB, boost_min_free_memory_bytes: GB })).toEqual({});
  });

  it.each<[SettingName, string[], string]>([
    ['reserved_domain_prefixes', ['Admin'], 'listItem'],
    ['reserved_domain_prefixes', ['-api'], 'listItem'],
    ['reserved_domain_prefixes', ['a'.repeat(31)], 'listItem'],
    ['reserved_domain_prefixes', ['api', 'api'], 'listUnique'],
    ['blocked_domain_prefix_patterns', ['preview-['], 'listItem'],
    ['blocked_domain_prefix_patterns', ['a.*', 'a.*'], 'listUnique'],
    ['git_allowed_hosts', ['https://github.com'], 'listItem'],
    ['git_allowed_hosts', ['github.com/org'], 'listItem'],
    ['git_allowed_hosts', ['.github.com'], 'listItem'],
    ['git_allowed_hosts', ['git hub.com'], 'listItem'],
    ['git_allowed_hosts', ['GitHub.com', 'github.com'], 'listUnique'],
    ['telemetry_allowed_exec_commands', ['/bin/sh', '/bin/sh'], 'listUnique'],
  ])('rejects the list %s = %j as %s', (name, value, code) => {
    expect(errorsOf({ [name]: value })).toEqual({ [name]: code });
  });

  it('accepts list values in the form the server accepts', () => {
    expect(errorsOf({
      reserved_domain_prefixes: ['a', 'api-2', 'x'.repeat(30)],
      blocked_domain_prefix_patterns: ['preview-[0-9]+', 'test.*'],
      git_allowed_hosts: ['github.com', 'git.example.org'],
      telemetry_allowed_exec_commands: ['/bin/sh', '/bin/bash'],
    })).toEqual({});
  });

  it('requires an allowed Git host only while Git sources are on', () => {
    expect(errorsOf({ git_sources_enabled: true, git_allowed_hosts: [] })).toEqual({ git_allowed_hosts: 'listRequired' });
    expect(errorsOf({ git_sources_enabled: false, git_allowed_hosts: [] })).toEqual({});
  });
});
