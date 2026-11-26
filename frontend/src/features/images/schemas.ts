import { z } from 'zod';
import type { TFunction } from '@/shared/i18n';

export const createBuildSchema = (t: TFunction) => z.object({
  source: z.enum(['archive', 'git']),
  repo_url: z.string(),
  ref: z.string(),
  tag: z.string().trim().min(1, t('validation.imageRequired')),
  context: z.string(),
  dockerfile: z.string(),
  build_args: z.array(z.object({
    key: z.string().trim().min(1, t('validation.keyRequired')),
    value: z.string(),
  })),
});

export type CreateBuildForm = z.input<ReturnType<typeof createBuildSchema>>;
