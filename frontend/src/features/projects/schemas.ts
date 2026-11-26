import { z } from 'zod';
import type { TFunction } from '@/shared/i18n';

// The project names Core accepts: a letter or digit, then up to 62 letters,
// digits, underscores, dots or hyphens.
export const projectNamePattern = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$/;
export const maxProjectNameLength = 63;

export const deployProjectSchema = (t: TFunction) => z.object({
  source: z.enum(['upload', 'git']),
  project_name: z.string().trim()
    .min(1, t('validation.projectNameRequired'))
    .regex(projectNamePattern, t('validation.projectNameFormat')),
  repo_url: z.string(),
  ref: z.string(),
  compose_file: z.string(),
});

export type DeployProjectSchemaForm = z.input<ReturnType<typeof deployProjectSchema>>;
