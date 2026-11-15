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
    dcm_disk_used_bytes: stats?.dcm_disk_used_bytes ?? 0,
    dcm_reserved_volume_bytes: stats?.dcm_reserved_volume_bytes ?? 0,
    host_min_free_disk_bytes: stats?.host_min_free_disk_bytes ?? 0,
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
