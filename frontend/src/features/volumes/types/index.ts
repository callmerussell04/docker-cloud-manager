import { z } from 'zod';

export interface VolumeData {
  id: string;
  status: string;
  last_error: string;
  used_bytes: number;
  usage_observed_at: number;
  created_at: number;
}

export interface AdminVolumeData extends VolumeData {
  docker_name: string;
  owner_id?: string;
  owner_username?: string;
}

export const createVolumeSchema = z.object({
  name: z.string().min(1, 'Имя обязательно')
});

export type CreateVolumeForm = z.input<typeof createVolumeSchema>;

export interface CreateVolumeDTO {
  name: string;
}
