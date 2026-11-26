import { knownErrorCodes } from '@/shared/api/apiError';
import type { TranslationKey } from '@/shared/i18n';
import { sortRows, type ListSort } from '@/shared/lib/listState';

export interface ActionCount {
  action: string;
  count: number;
}

export interface ReportsOverview {
  from: number;
  to: number;
  audit_events_total: number;
  failed_actions_total: number;
  active_users_total: number;
  memory_usage_bytes: number;
  reserved_memory_bytes: number;
  cpu_percent: number;
  total_disk_bytes: number;
  resources_total: number;
  containers_total: number;
  containers_running: number;
  volumes_total: number;
  images_total: number;
  builds_total: number;
  projects_total: number;
  last_usage_snapshot_at: number;
  usage_snapshot_interval_seconds: number;
  top_actions: ActionCount[];
}

export interface RefreshUsageSnapshotsResponse {
  bucket_start: number;
  collected_at: number;
  snapshots_count: number;
}

export interface UserUsageReportItem {
  owner_id: string;
  owner_username: string;
  memory_usage_bytes: number;
  reserved_memory_bytes: number;
  cpu_percent: number;
  total_disk_bytes: number;
  resources_total: number;
  containers_total: number;
  containers_running: number;
  volumes_total: number;
  images_total: number;
  builds_total: number;
  projects_total: number;
  actions_total: number;
}

export interface UserUsageReportResponse {
  users: UserUsageReportItem[];
  total_count: number;
}

export interface UserUsagePoint {
  bucket_start: number;
  memory_usage_bytes: number;
  reserved_memory_bytes: number;
  cpu_percent: number;
  total_disk_bytes: number;
  resources_total: number;
  containers_total: number;
  containers_running: number;
  volumes_total: number;
  images_total: number;
  builds_total: number;
  projects_total: number;
  actions_total: number;
}

export interface UserUsageTimelineResponse {
  points: UserUsagePoint[];
}

export interface AuditEvent {
  id: string;
  occurred_at: number;
  actor_user_id: string;
  actor_username: string;
  actor_scope: string;
  action: string;
  outcome: string;
  resource_type: string;
  resource_id: string;
  resource_name: string;
  owner_id: string;
  owner_username: string;
  request_id: string;
  client_ip: string;
  user_agent: string;
  error_code: string;
  details_json: string;
}

export interface AuditEventsResponse {
  events: AuditEvent[];
  total_count: number;
}

export interface ReportRangeParams {
  from: number;
  to: number;
}

// AuditFilters reads audit events. The window is optional: without a `to`
// the server reads up to now, and auditLogStart as `from` reads the whole
// log (a `from` of 0 counts as unset, which the server takes as the last
// 24 hours).
export interface AuditFilters extends Partial<ReportRangeParams> {
  actor_user_id?: string;
  action?: string;
  outcome?: string;
  resource_type?: string;
  search?: string;
  page: number;
  limit: number;
}

// auditLogStart is the first second of Unix time, the `from` that reads the
// audit log from its beginning.
export const auditLogStart = 1;

export interface UserUsageFilters extends ReportRangeParams {
  sort: string;
  search?: string;
  page: number;
  limit: number;
}

// auditDetails returns the event details worth showing, in display order.
export function auditDetails(event: AuditEvent) {
  if (!event.details_json || event.details_json === '{}') {
    return [];
  }
  try {
    const parsed = JSON.parse(event.details_json) as Record<string, unknown>;
    const details: Array<[string, unknown]> = [
      ['full_domain', parsed.full_domain],
      ['domain_prefix', parsed.domain_prefix],
      ['internal_port', parsed.internal_port],
      ['container_name', parsed.container_name],
      ['container_id', parsed.container_id],
      ['project_name', parsed.project_name],
      ['project_id', parsed.project_id],
      ['compose_service', parsed.compose_service],
      ['previous_domain_prefix', parsed.previous_domain_prefix],
      ['previous_internal_port', parsed.previous_internal_port],
      ['source_type', parsed.source_type],
    ];
    return details
      .filter(([, value]) => value !== undefined && value !== null && String(value) !== '')
      .map(([key, value]) => ({ key, value: String(value) }));
  } catch {
    return [{ key: 'details', value: event.details_json }];
  }
}

// seriesCount is how many chart series tokens there are (series-1 to -5).
const seriesCount = 5;

// userSeries gives each user the chart series (1 to 5) they are drawn in
// throughout the reports: by the order of their ids, never by rank, so
// sorting or a changed figure never repaints anyone. Past the fifth user the
// series repeat; the legend and the direct labels name each one.
export function userSeries(ownerIds: readonly string[]): Map<string, number> {
  const ordered = [...new Set(ownerIds)].sort();
  return new Map(ordered.map((id, index) => [id, (index % seriesCount) + 1]));
}

// UsageMeasure is a figure the usage charts draw per user over time.
export type UsageMeasure = 'reserved' | 'used' | 'cpu' | 'disk';

const measureOf: Record<UsageMeasure, (point: UserUsagePoint) => number> = {
  reserved: (point) => point.reserved_memory_bytes,
  used: (point) => point.memory_usage_bytes,
  cpu: (point) => point.cpu_percent,
  disk: (point) => point.total_disk_bytes,
};

// maxUsageSlots is how many points a usage chart draws at most.
const maxUsageSlots = 120;
const fallbackIntervalSeconds = 300;

// usageSlotSeconds is how wide one point of a usage chart is: the collection
// interval, widened by whole intervals until the range holds at most
// maxUsageSlots of them.
export function usageSlotSeconds(rangeSeconds: number, intervalSeconds: number) {
  const interval = intervalSeconds > 0 ? intervalSeconds : fallbackIntervalSeconds;
  return Math.max(1, Math.ceil(rangeSeconds / maxUsageSlots / interval)) * interval;
}

export interface UserTimeline {
  ownerId: string;
  points: readonly UserUsagePoint[];
}

export interface UsageSeries {
  // times are the slots' starts (Unix seconds), oldest first.
  times: number[];
  // values holds each user's figure per slot, in the order of times.
  values: Map<string, number[]>;
  // totals is every user's figure added up per slot.
  totals: number[];
}

// usageSeries lines up the users' timelines on common slots of slotSeconds:
// in each slot a user's figure is their latest reading there, or 0 when they
// have none while others do. A slot nobody was collected in is left out, so
// the chart joins the readings either side rather than dropping to zero.
export function usageSeries(timelines: readonly UserTimeline[], measure: UsageMeasure, slotSeconds: number): UsageSeries {
  const read = measureOf[measure];
  const latest = new Map<string, Map<number, UserUsagePoint>>();
  const slots = new Set<number>();
  for (const { ownerId, points } of timelines) {
    const bySlot = new Map<number, UserUsagePoint>();
    for (const point of points) {
      const slot = Math.floor(point.bucket_start / slotSeconds) * slotSeconds;
      const held = bySlot.get(slot);
      if (!held || held.bucket_start < point.bucket_start) bySlot.set(slot, point);
      slots.add(slot);
    }
    latest.set(ownerId, bySlot);
  }
  const times = [...slots].sort((a, b) => a - b);
  const values = new Map<string, number[]>();
  for (const { ownerId } of timelines) {
    const bySlot = latest.get(ownerId);
    values.set(ownerId, times.map((time) => {
      const point = bySlot?.get(time);
      return point ? read(point) : 0;
    }));
  }
  const totals = times.map((_, index) => [...values.values()].reduce((sum, row) => sum + row[index], 0));
  return { times, values, totals };
}

// The audited proxy (uploads and Git deploys) records the HTTP status text
// instead of a code.
const statusTextKeys: Record<string, TranslationKey> = {
  'Bad Request': 'apiError.bad_request',
  'Unprocessable Entity': 'apiError.bad_request',
  Unauthorized: 'apiError.unauthorized',
  Forbidden: 'apiError.forbidden',
  'Not Found': 'apiError.not_found',
  Conflict: 'apiError.conflict',
  'Request Entity Too Large': 'admin.reports.reason.tooLarge',
  'Too Many Requests': 'admin.reports.reason.tooMany',
  'Request Timeout': 'apiError.timeout',
  'Gateway Timeout': 'apiError.timeout',
  'Bad Gateway': 'apiError.unavailable',
  'Service Unavailable': 'apiError.unavailable',
  'Insufficient Storage': 'apiError.resource_exhausted',
  'Internal Server Error': 'apiError.internal',
};

// auditFailureKey is the message a failed action's reason is shown by: its
// recorded error code mapped to a translated message, never the raw code.
export function auditFailureKey(errorCode: string): TranslationKey {
  if (!errorCode) return 'admin.reports.reason.unrecorded';
  // The error codes Core and Gateway record on a failed action
  // (apperrors.Code) are the API's own, each shown by its message.
  if (knownErrorCodes.has(errorCode)) return `apiError.${errorCode}` as TranslationKey;
  return statusTextKeys[errorCode] ?? 'admin.reports.reason.other';
}

// The report ranges, as the URL names them (?range=24h); 7 days is the
// default and left out.
export const reportRanges = ['24h', '7d', '30d'] as const;
export type ReportRange = (typeof reportRanges)[number];
export const defaultReportRange: ReportRange = '7d';

export const rangeSeconds: Record<ReportRange, number> = {
  '24h': 24 * 60 * 60,
  '7d': 7 * 24 * 60 * 60,
  '30d': 30 * 24 * 60 * 60,
};

export function readReportRange(value: string | null): ReportRange {
  return reportRanges.find((range) => range === value) ?? defaultReportRange;
}

// failureRate is the share of the range's actions that failed, in percent.
export function failureRate(failed: number, total: number) {
  return total > 0 ? (failed / total) * 100 : 0;
}

// The users table's columns, each of which sorts it.
export const usageSortKeys = ['user', 'trend', 'reserved', 'used', 'cpu', 'disk', 'actions'] as const;
export type UsageSortKey = (typeof usageSortKeys)[number];
export const defaultUsageSort: ListSort<UsageSortKey> = { key: 'reserved', ascending: false };

// nextUsageSort is the table's sort after a column label is clicked: the
// column already chosen reverses; a newly chosen figure ranks the largest
// first, and names sort from A.
export function nextUsageSort(current: ListSort<UsageSortKey>, key: UsageSortKey): ListSort<UsageSortKey> {
  return { key, ascending: current.key === key ? !current.ascending : key === 'user' };
}

// trendChange is how much a figure grew over the range: its last reading
// less its first.
export function trendChange(values: readonly number[]) {
  return values.length >= 2 ? values[values.length - 1] - values[0] : 0;
}

const usageSortValue: Record<Exclude<UsageSortKey, 'user' | 'trend'>, (row: UserUsageReportItem) => number> = {
  reserved: (row) => row.reserved_memory_bytes,
  used: (row) => row.memory_usage_bytes,
  cpu: (row) => row.cpu_percent,
  disk: (row) => row.total_disk_bytes,
  actions: (row) => row.actions_total,
};

// sortUsageUsers orders the users table by a column; equal figures keep
// their names' order either way. The trend sorts by trendChange, given per
// user in trends.
export function sortUsageUsers(
  rows: readonly UserUsageReportItem[],
  sort: ListSort<UsageSortKey>,
  trends: ReadonlyMap<string, number>,
): UserUsageReportItem[] {
  const name = (row: UserUsageReportItem) => row.owner_username || row.owner_id;
  if (sort.key === 'user') return sortRows(rows, (row) => [name(row), row.owner_id], sort.ascending);
  const figure = sort.key === 'trend' ? (row: UserUsageReportItem) => trends.get(row.owner_id) ?? 0 : usageSortValue[sort.key];
  return sortRows(rows, (row) => [figure(row), name(row)], sort.ascending);
}
