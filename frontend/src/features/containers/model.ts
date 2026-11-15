import type { Scope } from '@/shared/api/scope';
import type { BadgeVariant } from '@/shared/ui/Badge';
import { labelFor, type TFunction, type TranslationKey } from '@/shared/i18n';

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

// ContainerOf is the container a list returns in a scope; the all-users view
// adds the owner and the Docker ID.
export type ContainerOf<S extends Scope> = S extends 'all' ? AdminContainerData : ContainerData;

export type ContainerAction = 'start' | 'stop' | 'delete';

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

const badgeVariants: Record<string, BadgeVariant> = {
  running: 'success',
  pending: 'info',
  created: 'default',
  creating: 'warning',
  starting: 'info',
  stopping: 'warning',
  exposing: 'info',
  exited: 'default',
  deleting: 'warning',
  missing: 'error',
  reconciling: 'warning',
  error: 'error',
};

// containerBadgeVariant is the badge a container status shows on every screen.
export function containerBadgeVariant(status: string): BadgeVariant {
  return badgeVariants[status] ?? 'info';
}

// Transitional statuses: a background worker is still changing the container,
// or its state is being reconciled with Docker.
const busyStatuses = new Set(['pending', 'creating', 'starting', 'stopping', 'exposing', 'deleting', 'missing', 'reconciling']);

// isContainerBusy reports whether a container is in a transitional state. Lists
// keep polling and lifecycle buttons stay disabled while it is.
export function isContainerBusy(status: string) {
  return busyStatuses.has(status);
}

// containerNeedsPolling reports whether a list showing this container should
// keep refreshing: while it is busy, or while an auto-stop countdown runs.
export function containerNeedsPolling(container: ContainerData) {
  return isContainerBusy(container.status) || (container.status === 'running' && !!container.ttl_deadline);
}

export interface BlockedContainerActions {
  start: boolean;
  stop: boolean;
  expose: boolean;
  delete: boolean;
  terminal: boolean;
}

// blockedContainerActions reports which actions a container's status rules
// out. Start waits until the container is settled and not running; stop and
// the terminal need a running container; routing changes wait until it is
// settled; delete is refused only while routing changes or deletion run.
export function blockedContainerActions(status: string): BlockedContainerActions {
  const busy = isContainerBusy(status);
  const running = status === 'running';
  return {
    start: running || busy,
    stop: !running,
    expose: busy,
    delete: status === 'exposing' || status === 'deleting',
    terminal: !running,
  };
}

// borrowedMemoryBytes is how far a container's current memory limit is above
// its standard limit: memory lent from idle host capacity that the platform
// may reclaim. It is 0 when the standard limit is unknown, as it is for a
// container that is not running.
export function borrowedMemoryBytes(stats?: ContainerStats): number {
  const standard = stats?.memory_standard_limit_bytes ?? 0;
  if (standard <= 0) {
    return 0;
  }
  return Math.max((stats?.memory_limit_bytes ?? 0) - standard, 0);
}

const statusKeys: Record<string, TranslationKey> = {
  running: 'status.running',
  exited: 'status.exited',
  created: 'status.created',
  creating: 'status.creating',
  starting: 'status.starting',
  stopping: 'status.stopping',
  deleting: 'status.deleting',
  missing: 'status.missing',
  reconciling: 'status.reconciling',
  error: 'status.error',
  pending: 'status.pending',
};

export function containerStatusLabel(t: TFunction, status?: string) {
  return labelFor(t, statusKeys, status);
}
