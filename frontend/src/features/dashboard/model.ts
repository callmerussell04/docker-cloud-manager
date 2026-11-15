export interface DashboardStats {
  containers_total: number;
  containers_running: number;
  containers_quota: number;
  ram_used_bytes: number;
  ram_quota_bytes: number;
  disk_used_mb: number;
  disk_quota_mb: number;
  volumes_total: number;
  volumes_quota: number;
  images_total: number;
  projects_total: number;
}