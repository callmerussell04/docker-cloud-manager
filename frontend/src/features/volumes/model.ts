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

export const MIB = 1024 * 1024;

export interface CreateVolumeDTO {
  name: string;
  size_bytes?: number;
}
