import type { PaginatedResponse } from '@/shared/api/pagination';
import type { Scope } from '@/shared/api/scope';
import type { StatusTone } from '@/shared/ui/StatusDot';
import type { ListSort, ListState } from '@/shared/lib/listState';
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
  // The memory and CPU the container is promised whenever it runs, chosen at
  // create or the platform defaults of that time.
  memory_reservation_bytes: number;
  cpu_reservation_millicores: number;
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
  // Absent takes the platform default.
  memory_reservation_mb?: number;
  cpu_reservation_millicores?: number;
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

// containerNeedsWatching reports whether a page showing one container should
// keep refreshing it: while it is busy, and while it runs, because a running
// container can stop on its own.
export function containerNeedsWatching(container: ContainerData) {
  return isContainerBusy(container.status) || container.status === 'running';
}

// holdsMemoryReservation reports whether a container's memory reservation
// counts against its owner's quota: while it runs or is on its way to
// running, unless Docker has lost it. It is Core's rule for the reserved
// memory the dashboard reports.
export function holdsMemoryReservation(container: ContainerData) {
  return container.status !== 'missing' && (container.status === 'running' || container.desired_status === 'running');
}

// containerTone is what a container's status means to the user: running is
// ok, error and missing are failing, exited and created are at rest, and every
// other status, including one this client does not know, is changing.
export function containerTone(status: string): StatusTone {
  if (status === 'running') return 'ok';
  if (status === 'error' || status === 'missing') return 'bad';
  if (status === 'exited' || status === 'created') return 'idle';
  return 'busy';
}

// ContainerFilter is the containers list's status filter.
export type ContainerFilter = 'all' | 'running' | 'stopped' | 'attention';

export const containerFilters: readonly ContainerFilter[] = ['all', 'running', 'stopped', 'attention'];

// ContainerSortKey is a containers list column.
export type ContainerSortKey = 'name' | 'status' | 'address' | 'memory' | 'created';

export const containerSortKeys: readonly ContainerSortKey[] = ['name', 'status', 'address', 'memory', 'created'];

// The list starts with failing containers first. The server filters, sorts,
// searches and counts the list (its status groups and order are Core's, the
// same as containerTone's), so it is exact at any size.
export const defaultContainerSort: ListSort<ContainerSortKey> = { key: 'status', ascending: true };

// containerSearchMaxLength is the longest search the server accepts, in
// characters.
export const containerSearchMaxLength = 100;

// ContainerListParams is the containers list's view as the API's query:
// each default is left out.
export interface ContainerListParams {
  status?: ContainerFilter;
  sort?: ContainerSortKey;
  order?: 'desc';
  q?: string;
}

// ContainerStatusCounts is how many containers each status filter shows,
// over the user's whole list (and the search), whatever the filter.
export type ContainerStatusCounts = Record<ContainerFilter, number>;

// ContainerPage is a page of the containers list with the count of every
// status filter.
export interface ContainerPage<C extends ContainerData> extends PaginatedResponse<C> {
  status_counts: ContainerStatusCounts;
}

// containerListParams turns the list's view into the query the server
// filters, sorts and searches by, leaving out All, the failing-first sort and
// a blank search; the search is trimmed and kept within what the server
// accepts.
export function containerListParams(view: { filter: ContainerFilter; sort: ListSort<ContainerSortKey>; search: string }): ContainerListParams {
  const params: ContainerListParams = {};
  if (view.filter !== 'all') params.status = view.filter;
  const isDefault = view.sort.key === defaultContainerSort.key && view.sort.ascending === defaultContainerSort.ascending;
  if (!isDefault) {
    params.sort = view.sort.key;
    if (!view.sort.ascending) params.order = 'desc';
  }
  const q = Array.from(view.search.trim()).slice(0, containerSearchMaxLength).join('');
  if (q) params.q = q;
  return params;
}

// containerViewChange is a change to the list's view, returning to the first
// page when the filter, sort or search changes: the server pages the list
// they shape, so the old page may no longer exist.
export function containerViewChange<C extends Partial<ListState<ContainerFilter, ContainerSortKey>>>(change: C): C {
  if (change.page !== undefined) return change;
  if (change.filter === undefined && change.sort === undefined && change.search === undefined) return change;
  return { ...change, page: 1 };
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
  exposing: 'status.exposing',
  deleting: 'status.deleting',
  missing: 'status.missing',
  reconciling: 'status.reconciling',
  error: 'status.error',
  pending: 'status.pending',
};

export function containerStatusLabel(t: TFunction, status?: string) {
  return labelFor(t, statusKeys, status);
}

// formatCores shows a CPU figure in millicores as cores, with up to three
// decimals ("0.25", "2").
export const formatCores = (millicores: number) => String(Number((millicores / 1000).toFixed(3)));

// UsageSample is one reading of a running container's stats, as the
// container page charts it.
export interface UsageSample {
  cpu: number;
  memory: number;
}

// usageHistoryLength is how many samples the container page keeps: about
// three minutes at the stats poll's three seconds.
export const usageHistoryLength = 60;

// appendUsageSample adds a sample to the history the page has taken since it
// opened, dropping the oldest once it is full. A history starts empty: the
// API reports only current usage, and none is invented.
export function appendUsageSample(history: readonly UsageSample[], sample: UsageSample): UsageSample[] {
  return [...history, sample].slice(-usageHistoryLength);
}

// ConsoleView is what the container page's console shows.
export type ConsoleView = 'logs' | 'terminal';

// consoleViewParam is the search parameter that holds it, as ?view=terminal.
export const consoleViewParam = 'view';

// consoleViewOf reads the console's view from the page's URL: the logs,
// unless the link asks for the terminal.
export function consoleViewOf(params: URLSearchParams): ConsoleView {
  return params.get(consoleViewParam) === 'terminal' ? 'terminal' : 'logs';
}

// consoleViewSearch is the search part of a link that opens the container
// page on a view; the logs are the default and need none.
export function consoleViewSearch(view: ConsoleView): string {
  return view === 'logs' ? '' : `?${consoleViewParam}=${view}`;
}

// containerPagePath is the link to a container's page under the list path
// its route lives at (/containers, or /admin/containers on the admin
// pages), opened on a view: the logs unless asked.
export function containerPagePath(listPath: string, id: string, view: ConsoleView = 'logs'): string {
  return `${listPath}/${id}${consoleViewSearch(view)}`;
}
