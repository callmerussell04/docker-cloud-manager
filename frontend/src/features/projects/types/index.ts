import { z } from 'zod';

export interface ProjectData {
  id: string;
  name: string;
  status: string;
  error_message: string;
  created_at: number;
}

export const createProjectSchema = z.object({
  project_name: z.string().min(1, 'Имя проекта обязательно'),
});

export type CreateProjectForm = z.input<typeof createProjectSchema>;