import { z } from 'zod';
import type { TFunction } from '@/lib/i18n';

export interface ProjectData {
  id: string;
  name: string;
  status: string;
  error_message: string;
  last_error: string;
  created_at: number;
}

export interface AdminProjectData extends ProjectData {
  owner_id?: string;
  owner_username?: string;
}
export const createProjectSchema = (t: TFunction) => z.object({
  project_name: z.string().min(1, t('validation.projectNameRequired')),
  repo_url: z.string().optional(),
  ref: z.string().optional(),
  compose_file: z.string().optional(),
});

export type CreateProjectForm = z.input<ReturnType<typeof createProjectSchema>>;

export interface CreateProjectGitPayload {
  project_name: string;
  repo_url: string;
  ref?: string;
  compose_file?: string;
}
