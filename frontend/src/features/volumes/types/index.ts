import { z } from 'zod';

export interface VolumeData {
  id: string;
  docker_name: string;
  driver: string;
  created_at: number;
  owner_id?: string;
  owner_username?: string;
}

export const createVolumeSchema = z.object({
  name: z.string().min(1, 'Имя обязательно'),
  driver: z.string().optional().default('local'),
  driver_opts: z.array(z.object({
    key: z.string().min(1, 'Ключ обязателен'),
    value: z.string()
  })).optional()
});

export type CreateVolumeForm = z.input<typeof createVolumeSchema>;

export interface CreateVolumeDTO {
  name: string;
  driver?: string;
  driver_opts?: Record<string, string>;
}