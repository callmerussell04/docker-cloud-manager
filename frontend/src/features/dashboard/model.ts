export interface DashboardStats {
  containers_total: number;
  containers_running: number;
  containers_quota: number;
  ram_used_bytes: number;
  ram_quota_bytes: number;
  cpu_used_millicores: number;
  cpu_quota_millicores: number;
  disk_used_mb: number;
  disk_quota_mb: number;
  volumes_total: number;
  volumes_quota: number;
  images_total: number;
  projects_total: number;
}
// millicoresToCores is the CPU figure the dashboard shows: cores, without
// trailing zeros.
export function millicoresToCores(millicores: number): number {
  return Number((millicores / 1000).toFixed(3));
}
