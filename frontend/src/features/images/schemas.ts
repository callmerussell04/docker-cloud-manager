import { z } from 'zod';
import type { TFunction } from '@/shared/i18n';

export const createBuildSchema = (t: TFunction) => z.object({
  tag: z.string().min(1, t('validation.imageRequired')),
  context: z.string().optional().default('.'),
  dockerfile: z.string().optional().default('Dockerfile'),
  repo_url: z.string().optional(),
  ref: z.string().optional(),
  build_args: z.array(z.object({
    key: z.string().min(1, t('validation.keyRequired')),
    value: z.string()
  })).optional(),
});

export type CreateBuildForm = z.input<ReturnType<typeof createBuildSchema>>;
