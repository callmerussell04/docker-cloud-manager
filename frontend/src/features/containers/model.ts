export interface ContainerData {
  id: string;
  project_id?: string;
  name: string;
  image_tag: string;
  internal_port: number;
  domain_prefix: string;
  status: string;
  desired_status?: string;
  last_error: string;
  last_exit_code?: number;
  ttl_deadline?: number;
  created_at: number;
}

export interface AdminContainerData extends ContainerData {
  docker_id: string;
  owner_id?: string;
  owner_username?: string;
}

export interface ContainerStats {
  cpu_percentage: number;
  memory_usage_bytes: number;
  memory_limit_bytes: number;
  // The limit create and start apply; a current limit above it is borrowed.
  memory_standard_limit_bytes: number;
  network_rx_bytes: number;
  network_tx_bytes: number;
}

export interface TelemetryTicketResponse {
  ticket: string;
  expires_at: number;
}

export interface CreateContainerDTO {
  name: string;
  image_tag: string;
  internal_port?: number;
  domain_prefix?: string;
  env_vars?: Record<string, string>;
  volume_mounts?: Array<{
    volume_id: string;
    mount_path: string;
    is_readonly: boolean;
  }>;
}

export interface ExposeContainerDTO {
  domain_prefix: string;
  internal_port: number;
}
