import type { Scope } from '@/shared/api/scope';

export interface VolumeData {
  id: string;
  name: string;
  status: string;
  last_error: string;
  used_bytes: number;
  usage_observed_at: number;
  size_bytes: number;
  size_enforced: boolean;
  created_at: number;
}

export interface AdminVolumeData extends VolumeData {
  docker_name: string;
  owner_id?: string;
  owner_username?: string;
}

// VolumeOf is the volume a list returns in a scope; the all-users view adds
// the owner and the Docker volume name.
export type VolumeOf<S extends Scope> = S extends 'all' ? AdminVolumeData : VolumeData;

export const MIB = 1024 * 1024;

export interface CreateVolumeDTO {
  name: string;
  size_bytes?: number;
}
