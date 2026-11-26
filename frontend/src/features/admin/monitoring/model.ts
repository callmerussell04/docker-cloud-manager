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
  // CPU every reserving container holds, and CPU the occupied build slots
  // hold, in millicores.
  dcm_reserved_cpu_millicores: number;
  dcm_reserved_build_cpu_millicores: number;
  dcm_disk_used_bytes: number;
  dcm_reserved_volume_bytes: number;
  dcm_unused_volume_promise_bytes: number;
  host_min_free_disk_bytes: number;
  // The cap the next build claim would get: max_concurrent_builds, or fewer
  // when the free disk above the floor cannot hold that many build slots.
  build_claim_cap: number;
  max_concurrent_builds: number;
  // Builds pending or running, queued ones included.
  builds_active: number;
  // The host's logical CPUs in millicores.
  cpu_total_millicores: number;
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

// withDefaults fills a missing or partial reading so the page renders zeros
// and the open admission state until the first reading arrives.
export function withDefaults(stats?: SystemMonitoring): SystemMonitoring {
  return {
    cpu_percent: stats?.cpu_percent ?? 0,
    memory_total_bytes: stats?.memory_total_bytes ?? 0,
    memory_used_bytes: stats?.memory_used_bytes ?? 0,
    memory_available_bytes: stats?.memory_available_bytes ?? 0,
    disk_total_bytes: stats?.disk_total_bytes ?? 0,
    disk_used_bytes: stats?.disk_used_bytes ?? 0,
    disk_free_bytes: stats?.disk_free_bytes ?? 0,
    dcm_reserved_memory_bytes: stats?.dcm_reserved_memory_bytes ?? 0,
    dcm_reserved_build_memory_bytes: stats?.dcm_reserved_build_memory_bytes ?? 0,
    dcm_borrowed_memory_bytes: stats?.dcm_borrowed_memory_bytes ?? 0,
    dcm_reserved_cpu_millicores: stats?.dcm_reserved_cpu_millicores ?? 0,
    dcm_reserved_build_cpu_millicores: stats?.dcm_reserved_build_cpu_millicores ?? 0,
    dcm_disk_used_bytes: stats?.dcm_disk_used_bytes ?? 0,
    dcm_reserved_volume_bytes: stats?.dcm_reserved_volume_bytes ?? 0,
    dcm_unused_volume_promise_bytes: stats?.dcm_unused_volume_promise_bytes ?? 0,
    host_min_free_disk_bytes: stats?.host_min_free_disk_bytes ?? 0,
    build_claim_cap: stats?.build_claim_cap ?? 0,
    max_concurrent_builds: stats?.max_concurrent_builds ?? 0,
    builds_active: stats?.builds_active ?? 0,
    cpu_total_millicores: stats?.cpu_total_millicores ?? 0,
    admission_status: stats?.admission_status ?? 'open',
    admission_reasons: stats?.admission_reasons ?? [],
    rootfs_quota_status: stats?.rootfs_quota_status ?? 'unknown',
    volume_size_mode: stats?.volume_size_mode ?? 'measured',
    containers_total: stats?.containers_total ?? 0,
    containers_running: stats?.containers_running ?? 0,
    containers_stopped: stats?.containers_stopped ?? 0,
    containers_error: stats?.containers_error ?? 0,
    containers_missing: stats?.containers_missing ?? 0,
    volumes_total: stats?.volumes_total ?? 0,
    images_total: stats?.images_total ?? 0,
    builds_total: stats?.builds_total ?? 0,
    projects_total: stats?.projects_total ?? 0,
    observed_at: stats?.observed_at ?? 0,
  };
}

// buildsHeldByDisk reports whether the disk, and not max_concurrent_builds,
// limits how many builds may run now.
export function buildsHeldByDisk(stats: SystemMonitoring): boolean {
  return stats.build_claim_cap < stats.max_concurrent_builds;
}

// CapacityPartKey names what holds part of a host resource.
export type CapacityPartKey = 'system' | 'containers' | 'builds' | 'lent' | 'dcm' | 'other';

// Capacity is a host resource split into what holds it, in order, and what
// is left free, never below zero.
export interface Capacity {
  total: number;
  parts: { key: CapacityPartKey; value: number }[];
  free: number;
}

function capacity(total: number, parts: { key: CapacityPartKey; value: number | undefined }[]): Capacity {
  const known = parts
    .filter((part): part is { key: CapacityPartKey; value: number } => part.value !== undefined)
    .map((part) => ({ key: part.key, value: Math.max(part.value, 0) }));
  const held = known.reduce((sum, part) => sum + part.value, 0);
  return { total, parts: known, free: Math.max(total - held, 0) };
}

// memoryCapacity splits the host's memory into the system reserve (a runtime
// setting; left out while it is unknown), what containers reserve, what the
// occupied build slots hold, and what running workloads borrow above their
// standard limits (lent), with the rest free. Reservations may be
// overcommitted past the host's memory; nothing is free then.
export function memoryCapacity(stats: SystemMonitoring, systemReserveBytes: number | undefined): Capacity {
  return capacity(stats.memory_total_bytes, [
    { key: 'system', value: systemReserveBytes },
    { key: 'containers', value: stats.dcm_reserved_memory_bytes },
    { key: 'builds', value: stats.dcm_reserved_build_memory_bytes },
    { key: 'lent', value: stats.dcm_borrowed_memory_bytes },
  ]);
}

// cpuCapacity splits the host's CPU, in millicores, into the system reserve
// (left out while unknown), what containers reserve and what the occupied
// build slots hold, with the rest free.
export function cpuCapacity(stats: SystemMonitoring, systemReserveMillicores: number | undefined): Capacity {
  return capacity(stats.cpu_total_millicores, [
    { key: 'system', value: systemReserveMillicores },
    { key: 'containers', value: stats.dcm_reserved_cpu_millicores },
    { key: 'builds', value: stats.dcm_reserved_build_cpu_millicores },
  ]);
}

// diskCapacity splits Docker's data disk into what DCM's images and volumes
// use and everything else used, with the rest free. DCM's figure is measured
// apart from the disk's, so it is cut at what the disk has used.
export function diskCapacity(stats: SystemMonitoring): Capacity {
  const used = Math.max(stats.disk_used_bytes, 0);
  const dcm = Math.min(Math.max(stats.dcm_disk_used_bytes, 0), used);
  return capacity(stats.disk_total_bytes, [
    { key: 'dcm', value: dcm },
    { key: 'other', value: used - dcm },
  ]);
}

// buildSlotsInUse is how many build slots hold host capacity: the active
// builds, up to max_concurrent_builds, as Core reserves them.
export function buildSlotsInUse(stats: SystemMonitoring): number {
  return Math.max(Math.min(stats.builds_active, stats.max_concurrent_builds), 0);
}

// HealthMark is how a health check reads: good, worth a look, failing, or
// not known.
export type HealthMark = 'ok' | 'warn' | 'bad' | 'unknown';

export type HealthCheck =
  | { id: 'admission'; state: 'open' | 'blocked'; mark: HealthMark }
  | { id: 'containerDisk'; state: RootfsQuotaStatus; mark: HealthMark }
  | { id: 'volumeSizes'; state: VolumeSizeMode; mark: HealthMark }
  | { id: 'builds'; state: 'free' | 'heldByDisk'; mark: HealthMark };

const containerDiskMarks: Record<RootfsQuotaStatus, HealthMark> = { enforced: 'ok', not_enforced: 'warn', unknown: 'unknown' };
const volumeSizeMarks: Record<VolumeSizeMode, HealthMark> = { enforced: 'ok', measured: 'warn', unavailable: 'warn' };

// healthChecks are the host's checks in page order: whether new work is
// admitted, whether container disk limits and volume sizes are enforced, and
// whether the disk holds builds below max_concurrent_builds. A status the
// page does not know reads as unknown (container disk) or measured (volumes).
export function healthChecks(stats: SystemMonitoring): HealthCheck[] {
  const blocked = stats.admission_status === 'blocked';
  const containerDisk: RootfsQuotaStatus = stats.rootfs_quota_status in containerDiskMarks ? stats.rootfs_quota_status : 'unknown';
  const volumeSizes: VolumeSizeMode = stats.volume_size_mode in volumeSizeMarks ? stats.volume_size_mode : 'measured';
  const held = buildsHeldByDisk(stats);
  return [
    { id: 'admission', state: blocked ? 'blocked' : 'open', mark: blocked ? 'bad' : 'ok' },
    { id: 'containerDisk', state: containerDisk, mark: containerDiskMarks[containerDisk] },
    { id: 'volumeSizes', state: volumeSizes, mark: volumeSizeMarks[volumeSizes] },
    { id: 'builds', state: held ? 'heldByDisk' : 'free', mark: held ? 'warn' : 'ok' },
  ];
}

// The monitoring page samples the host every vitalsIntervalMs while it is
// open and keeps the newest vitalsHistoryLength samples: about 90 s.
export const vitalsIntervalMs = 2000;
export const vitalsHistoryLength = 45;

// VitalSample is one reading of the host: CPU busy in percent and memory in
// use in bytes.
export interface VitalSample {
  cpu: number;
  memory: number;
}

// appendVitalSample adds a sample after the history the page has taken since
// it opened, dropping the oldest once the history is full.
export function appendVitalSample(history: readonly VitalSample[], sample: VitalSample): VitalSample[] {
  return [...history, sample].slice(-vitalsHistoryLength);
}

// buildsQueued is how many active builds wait for a build slot.
export function buildsQueued(stats: SystemMonitoring): number {
  return Math.max(stats.builds_active - buildSlotsInUse(stats), 0);
}
