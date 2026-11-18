/** The runtime settings, as `/admin/config` returns and accepts them. */
export interface SystemConfig {
  default_user_quota_cpu: number;
  default_user_quota_ram_mb: number;
  default_user_quota_disk_mb: number;
  max_containers_per_user: number;
  max_volumes_per_user: number;
  max_queued_builds_per_user: number;
  max_queued_compose_deploys_per_user: number;
  max_staged_source_bytes_per_user: number;

  default_memory_reservation_bytes: number;
  max_burst_multiplier: number;
  memory_boost_multiplier: number;
  boost_min_free_memory_bytes: number;
  memory_pressure_min_free_bytes: number;
  reserved_system_memory_bytes: number;
  overcommit_factor: number;

  default_cpu_reservation_millicores: number;
  max_cpu_burst_multiplier: number;
  cpu_boost_multiplier: number;
  reserved_system_cpu_millicores: number;
  cpu_overcommit_factor: number;

  container_disk_quota: string;
  max_log_size: string;
  default_volume_size_bytes: number;
  volume_size_enforcement: boolean;
  host_min_free_disk_bytes: number;

  container_ttl_hours: number;
  container_stop_timeout: number;
  container_pids_limit: number;

  image_builds_enabled: boolean;
  max_concurrent_builds: number;
  build_memory_bytes: number;
  build_cpu_millicores: number;
  build_max_disk_bytes: number;
  max_build_time_minutes: number;
  max_build_log_size_bytes: number;
  max_archive_size_bytes: number;
  max_unpacked_size_bytes: number;

  git_sources_enabled: boolean;
  git_allowed_hosts: string[];
  git_clone_timeout_seconds: number;
  git_max_repository_bytes: number;

  compose_upload_max_bytes: number;
  compose_pipeline_timeout_minutes: number;
  compose_dependency_wait_timeout_minutes: number;

  reserved_domain_prefixes: string[];
  blocked_domain_prefix_patterns: string[];

  telemetry_allowed_exec_commands: string[];
  telemetry_terminal_idle_timeout_seconds: number;
  telemetry_terminal_max_duration_seconds: number;
}

export type SettingName = keyof SystemConfig;

export const byteUnits = [
  { label: 'Bytes', multiplier: 1 },
  { label: 'KB', multiplier: 1024 },
  { label: 'MB', multiplier: 1024 ** 2 },
  { label: 'GB', multiplier: 1024 ** 3 },
] as const;

export type ByteUnit = (typeof byteUnits)[number]['label'];

function multiplierOf(unit: ByteUnit) {
  return byteUnits.find((item) => item.label === unit)?.multiplier ?? 1;
}

/** The largest unit that shows the size as a whole number. Zero is shown in MB. */
export function byteUnitFor(bytes: number): ByteUnit {
  if (bytes === 0) return 'MB';
  for (const unit of ['GB', 'MB', 'KB'] as const) {
    if (bytes >= multiplierOf(unit) && bytes % multiplierOf(unit) === 0) return unit;
  }
  return 'Bytes';
}

export function bytesToUnit(bytes: number, unit: ByteUnit) {
  return bytes / multiplierOf(unit);
}

export function unitToBytes(value: number, unit: ByteUnit) {
  return Math.round(value * multiplierOf(unit));
}

/** A size for reading, in the unit of its magnitude with up to two decimals. Zero is shown in MB. */
export function formatBytes(bytes: number) {
  const unit = bytes === 0
    ? 'MB'
    : ([...byteUnits].reverse().find((item) => bytes >= item.multiplier)?.label ?? 'Bytes');
  return `${Number(bytesToUnit(bytes, unit).toFixed(2))} ${unit}`;
}

export function millicoresToCores(millicores: number) {
  return millicores / 1000;
}

export function coresToMillicores(cores: number) {
  return Math.round(cores * 1000);
}

/**
 * Whether boost can raise limits: `on`; `off` when the boost multiplier is not
 * above the limit multiplier; `pressureOff` when a zero memory pressure floor
 * turns the worker that does both memory and CPU boost off.
 */
export type BoostState = 'on' | 'off' | 'pressureOff';

function boostState(limitMultiplier: number, boostMultiplier: number, pressureFloorBytes: number): BoostState {
  if (pressureFloorBytes <= 0) return 'pressureOff';
  return boostMultiplier > limitMultiplier ? 'on' : 'off';
}

function allNumbers(values: Array<number | undefined>): values is number[] {
  return values.every((value) => typeof value === 'number' && Number.isFinite(value));
}

export interface MemorySummary {
  reservationBytes: number;
  standardLimitBytes: number;
  boostCeilingBytes: number;
  boost: BoostState;
}

/** What a container with the default reservation gets; null while a value is missing. */
export function memorySummary(
  settings: Partial<Pick<SystemConfig,
    'default_memory_reservation_bytes' | 'max_burst_multiplier' | 'memory_boost_multiplier' | 'memory_pressure_min_free_bytes'>>,
): MemorySummary | null {
  const values = [
    settings.default_memory_reservation_bytes,
    settings.max_burst_multiplier,
    settings.memory_boost_multiplier,
    settings.memory_pressure_min_free_bytes,
  ];
  if (!allNumbers(values)) return null;
  const [reservation, limitMultiplier, boostMultiplier, pressureFloor] = values;
  const boost = boostState(limitMultiplier, boostMultiplier, pressureFloor);
  const standardLimitBytes = reservation * limitMultiplier;
  return {
    reservationBytes: reservation,
    standardLimitBytes,
    boostCeilingBytes: boost === 'on' ? reservation * boostMultiplier : standardLimitBytes,
    boost,
  };
}

export interface CPUSummary {
  reservationCores: number;
  standardLimitCores: number;
  boostCeilingCores: number;
  boost: BoostState;
}

export function cpuSummary(
  settings: Partial<Pick<SystemConfig,
    'default_cpu_reservation_millicores' | 'max_cpu_burst_multiplier' | 'cpu_boost_multiplier' | 'memory_pressure_min_free_bytes'>>,
): CPUSummary | null {
  const values = [
    settings.default_cpu_reservation_millicores,
    settings.max_cpu_burst_multiplier,
    settings.cpu_boost_multiplier,
    settings.memory_pressure_min_free_bytes,
  ];
  if (!allNumbers(values)) return null;
  const [reservation, limitMultiplier, boostMultiplier, pressureFloor] = values;
  const boost = boostState(limitMultiplier, boostMultiplier, pressureFloor);
  const standardLimitCores = millicoresToCores(reservation * limitMultiplier);
  return {
    reservationCores: millicoresToCores(reservation),
    standardLimitCores,
    boostCeilingCores: boost === 'on' ? millicoresToCores(reservation * boostMultiplier) : standardLimitCores,
    boost,
  };
}

/** Which default quota runs out first for containers with the default reservations. */
export type CapacityLimit = 'memory' | 'cpu' | 'both';

export interface DefaultContainerCapacity {
  containers: number;
  limitedBy: CapacityLimit;
}

/**
 * How many containers with the default reservations the default quota allows,
 * and which quota limits them; null while a value is missing or a reservation
 * is not positive.
 */
export function defaultContainerCapacity(
  settings: Partial<Pick<SystemConfig,
    'default_user_quota_ram_mb' | 'default_user_quota_cpu' | 'default_memory_reservation_bytes' | 'default_cpu_reservation_millicores'>>,
): DefaultContainerCapacity | null {
  const values = [
    settings.default_user_quota_ram_mb,
    settings.default_user_quota_cpu,
    settings.default_memory_reservation_bytes,
    settings.default_cpu_reservation_millicores,
  ];
  if (!allNumbers(values)) return null;
  const [quotaMB, quotaCores, memoryReservation, cpuReservation] = values;
  if (memoryReservation <= 0 || cpuReservation <= 0) return null;
  const byMemory = Math.floor((quotaMB * 1024 ** 2) / memoryReservation);
  const byCPU = Math.floor(coresToMillicores(quotaCores) / cpuReservation);
  return {
    containers: Math.max(Math.min(byMemory, byCPU), 0),
    limitedBy: byMemory === byCPU ? 'both' : byMemory < byCPU ? 'memory' : 'cpu',
  };
}
