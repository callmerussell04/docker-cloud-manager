import { z } from 'zod';

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
export const createProjectSchema = z.object({
  project_name: z.string().min(1, 'Имя проекта обязательно'),
  repo_url: z.string().optional(),
  ref: z.string().optional(),
  compose_file: z.string().optional(),
});

export type CreateProjectForm = z.input<typeof createProjectSchema>;

export interface CreateProjectGitPayload {
  project_name: string;
  repo_url: string;
  ref?: string;
  compose_file?: string;
}
