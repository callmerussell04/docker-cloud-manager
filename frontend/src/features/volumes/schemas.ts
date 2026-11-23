import { z } from 'zod';
import type { TFunction } from '@/shared/i18n';

// isSizeMiB reports whether a typed size is a whole number of MiB, at least 1.
export function isSizeMiB(value: string) {
  return /^[1-9]\d*$/.test(value.trim());
}

// size_mib is optional: empty asks for the default size; otherwise a whole
// number of MiB, at least 1.
export const createVolumeSchema = (t: TFunction) => z.object({
  name: z.string().trim().min(1, t('validation.nameRequired')),
  size_mib: z
    .string()
    .optional()
    .refine((value) => !value?.trim() || isSizeMiB(value), t('volumes.sizeInvalid')),
});

export type CreateVolumeForm = z.input<ReturnType<typeof createVolumeSchema>>;
