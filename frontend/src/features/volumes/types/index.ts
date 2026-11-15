import { z } from 'zod';
import type { TFunction } from '@/shared/i18n';

export interface VolumeData {
  id: string;
  name: string;
  status: string;
  last_error: string;
  used_bytes: number;
  usage_observed_at: number;
  size_bytes: number;
  size_enforced: boolean;
  created_at: number;
}

export interface AdminVolumeData extends VolumeData {
  docker_name: string;
  owner_id?: string;
  owner_username?: string;
}

export const MIB = 1024 * 1024;

// size_mib is optional: empty asks for the default size; otherwise a whole
// number of MiB, at least 1.
export const createVolumeSchema = (t: TFunction) => z.object({
  name: z.string().min(1, t('validation.nameRequired')),
  size_mib: z
    .string()
    .optional()
    .refine((value) => !value?.trim() || /^[1-9]\d*$/.test(value.trim()), t('volumes.sizeInvalid')),
});

export type CreateVolumeForm = z.input<ReturnType<typeof createVolumeSchema>>;

export interface CreateVolumeDTO {
  name: string;
  size_bytes?: number;
}
