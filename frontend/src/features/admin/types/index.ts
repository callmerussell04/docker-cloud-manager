import { z } from 'zod';

export interface PaginatedResponse<T> {
  items: T[];
  total_count: number;
}

export interface SystemConfig {
  base_domain: string;
  default_memory_reservation_bytes: number;
  reserved_system_memory_bytes: number;
  overcommit_factor: number;
  max_burst_multiplier: number;
  default_cpu_shares: number;
  high_load_cpu_shares: number;
  high_load_container_count: number;
  container_stop_timeout: number;
  max_log_size: string;
  max_log_files: string;
  container_disk_quota: string;
  max_volumes_per_user: number;
  max_containers_per_user: number;
  registry_url: string;
  container_ttl_hours: number;
}

export const systemConfigSchema = z.object({
  base_domain: z.string().min(1, 'Обязательное поле'),
  default_memory_reservation_bytes: z.number().min(1),
  reserved_system_memory_bytes: z.number().min(0),
  overcommit_factor: z.number().min(1),
  max_burst_multiplier: z.number().min(1),
  default_cpu_shares: z.number().min(2),
  high_load_cpu_shares: z.number().min(2),
  high_load_container_count: z.number().min(1),
  container_stop_timeout: z.number().min(1),
  max_log_size: z.string().min(1),
  max_log_files: z.string().min(1),
  container_disk_quota: z.string().min(1),
  max_volumes_per_user: z.number().min(1),
  max_containers_per_user: z.number().min(1),
  registry_url: z.string().min(1),
  container_ttl_hours: z.number().min(0),
});

export type SystemConfigForm = z.input<typeof systemConfigSchema>;