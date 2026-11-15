import { z } from 'zod';
import type { TFunction } from '@/shared/i18n';

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
