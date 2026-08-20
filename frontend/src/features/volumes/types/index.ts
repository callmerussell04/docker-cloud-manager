import { z } from 'zod';
import type { TFunction } from '@/lib/i18n';

export interface VolumeData {
  id: string;
  name: string;
  status: string;
  last_error: string;
  used_bytes: number;
  usage_observed_at: number;
  created_at: number;
}

export interface AdminVolumeData extends VolumeData {
  docker_name: string;
  owner_id?: string;
  owner_username?: string;
}

export const createVolumeSchema = (t: TFunction) => z.object({
  name: z.string().min(1, t('validation.nameRequired'))
});

export type CreateVolumeForm = z.input<ReturnType<typeof createVolumeSchema>>;

export interface CreateVolumeDTO {
  name: string;
}
