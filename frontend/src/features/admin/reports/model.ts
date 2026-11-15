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

export interface AuditFilters extends ReportRangeParams {
  actor_user_id?: string;
  action?: string;
  outcome?: string;
  resource_type?: string;
  search?: string;
  page: number;
  limit: number;
}

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
