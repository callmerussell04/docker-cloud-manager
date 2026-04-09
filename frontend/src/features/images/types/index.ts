import { z } from 'zod';

export interface ImageData {
  id: string;
  tag: string;
  size_mb: number;
  is_custom: boolean;
  created_at: number;
  owner_id?: string;
  owner_username?: string;
}

export interface BuildData {
  id: string;
  image_id: string;
  status: string;
  started_at: number;
  finished_at: number;
  log_file_path: string;
  owner_id?: string;
  owner_username?: string;
}

export const createBuildSchema = z.object({
  tag: z.string().min(1, 'Укажите тег образа (например, my-app:v1)'),
  context: z.string().optional().default('.'),
  dockerfile: z.string().optional().default('Dockerfile'),
  build_args: z.array(z.object({
    key: z.string().min(1, 'Ключ обязателен'),
    value: z.string()
  })).optional(),
});

export type CreateBuildForm = z.input<typeof createBuildSchema>;