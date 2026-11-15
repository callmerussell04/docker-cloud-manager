import { z } from 'zod';
import type { TFunction } from '@/shared/i18n';

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

export const exposeContainerSchema = (t: TFunction) => z.object({
  domain_prefix: z.string().min(1, t('validation.prefixRequired')).max(30, t('validation.max30')),
  internal_port: z.union([z.string(), z.number()]).transform(v => Number(v)),
});
