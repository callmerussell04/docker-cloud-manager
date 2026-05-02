import { z } from 'zod';

export interface ContainerData {
  id: string;
  docker_id: string;
  name: string;
  image_tag: string;
  internal_port: number;
  domain_prefix: string;
  status: string;
  created_at: number;
  owner_id?: string;
  owner_username?: string;
}

export interface ContainerStats {
  cpu_percentage: number;
  memory_usage_bytes: number;
  memory_limit_bytes: number;
  network_rx_bytes: number;
  network_tx_bytes: number;
}

export interface TelemetryTicketResponse {
  ticket: string;
  expires_at: number;
}

export const createContainerSchema = z.object({
  name: z.string().min(1, 'Имя обязательно'),
  image_tag: z.string().min(1, 'Укажите образ'),
  internal_port: z.union([z.string(), z.number()]).optional().transform(v => v === '' ? undefined : Number(v)),
  domain_prefix: z.string().max(30, 'Максимум 30 символов').optional().transform(v => v === '' ? undefined : v),
  env_vars: z.array(z.object({
    key: z.string().min(1, 'Ключ обязателен'),
    value: z.string()
  })).optional(),
  volume_mounts: z.array(z.object({
    volume_id: z.string().min(1, 'Выберите том'),
    mount_path: z.string().min(1, 'Путь обязателен'),
    is_readonly: z.boolean()
  })).optional()
});

export type CreateContainerForm = z.input<typeof createContainerSchema>;

export interface CreateContainerDTO {
  name: string;
  image_tag: string;
  internal_port?: number;
  domain_prefix?: string;
  env_vars?: Record<string, string>;
  volume_mounts?: Array<{
    volume_id: string;
    mount_path: string;
    is_readonly: boolean;
  }>;
}

export const exposeContainerSchema = z.object({
  domain_prefix: z.string().min(1, 'Префикс обязателен').max(30, 'Максимум 30 символов'),
  internal_port: z.union([z.string(), z.number()]).transform(v => Number(v)),
});

export type ExposeContainerDTO = z.infer<typeof exposeContainerSchema>;