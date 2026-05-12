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
  reserved_memory_bytes: number;
  total_disk_bytes: number;
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
  reserved_memory_bytes: number;
  total_disk_bytes: number;
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
  reserved_memory_bytes: number;
  total_disk_bytes: number;
  containers_total: number;
  containers_running: number;
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
