import { z } from 'zod';
import type { TFunction } from '@/shared/i18n';

export const createProjectSchema = (t: TFunction) => z.object({
  project_name: z.string().min(1, t('validation.projectNameRequired')),
  repo_url: z.string().optional(),
  ref: z.string().optional(),
  compose_file: z.string().optional(),
});

export type CreateProjectForm = z.input<ReturnType<typeof createProjectSchema>>;
