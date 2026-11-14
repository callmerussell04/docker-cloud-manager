import { z } from 'zod';
import type { TFunction } from '@/lib/i18n';

export interface ImageData {
  id: string;
  tag: string;
  size_bytes: number;
  status: string;
  last_error: string;
  created_at: number;
}

export interface AdminImageData extends ImageData {
  owner_id?: string;
  owner_username?: string;
}

export interface BuildData {
  id: string;
  // Absent once the build's image record has been deleted.
  image_id?: string;
  // The tag the build was queued for.
  tag?: string;
  project_id?: string;
  project_service_name?: string;
  status: string;
  created_at: number;
  started_at: number;
  finished_at: number;
}

export interface AdminBuildData extends BuildData {
  log_object_key: string;
  owner_id?: string;
  owner_username?: string;
}

export interface BuildAvailability {
  enabled: boolean;
  message: string;
  git_sources_enabled?: boolean;
  git_message?: string;
}

export interface CreateBuildGitPayload {
  repo_url: string;
  ref?: string;
  tag: string;
  context?: string;
  dockerfile?: string;
  build_args?: Record<string, string>;
}

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
