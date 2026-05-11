import { z } from 'zod';
import type { TFunction } from '@/lib/i18n';

export interface ContainerData {
  id: string;
  name: string;
  image_tag: string;
  internal_port: number;
  domain_prefix: string;
  status: string;
  desired_status?: string;
  last_error: string;
  last_exit_code?: number;
  created_at: number;
}

export interface AdminContainerData extends ContainerData {
  docker_id: string;
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

export const createContainerSchema = (t: TFunction) => z.object({
  name: z.string().min(1, t('validation.nameRequired')),
  image_tag: z.string().min(1, t('validation.imageRequired')),
  internal_port: z.union([z.string(), z.number()]).optional().transform(v => v === '' ? undefined : Number(v)),
  domain_prefix: z.string().max(30, t('validation.max30')).optional().transform(v => v === '' ? undefined : v),
  env_vars: z.array(z.object({
    key: z.string().min(1, t('validation.keyRequired')),
    value: z.string()
  })).optional(),
  volume_mounts: z.array(z.object({
    volume_id: z.string().min(1, t('validation.volumeRequired')),
    mount_path: z.string().min(1, t('validation.pathRequired')),
    is_readonly: z.boolean()
  })).optional()
});

export type CreateContainerForm = z.input<ReturnType<typeof createContainerSchema>>;

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

export const exposeContainerSchema = (t: TFunction) => z.object({
  domain_prefix: z.string().min(1, t('validation.prefixRequired')).max(30, t('validation.max30')),
  internal_port: z.union([z.string(), z.number()]).transform(v => Number(v)),
});

export type ExposeContainerDTO = z.infer<ReturnType<typeof exposeContainerSchema>>;
