import { privateApi } from '@/api/axios';
import type {
  AuditEventsResponse,
  AuditFilters,
  ReportRangeParams,
  RefreshUsageSnapshotsResponse,
  ReportsOverview,
  UserUsageFilters,
  UserUsageReportResponse,
  UserUsageTimelineResponse,
} from '../types';

export const getReportsOverviewFn = async (params: ReportRangeParams): Promise<ReportsOverview> => {
  const response = await privateApi.get<ReportsOverview>('/admin/reports/overview', { params });
  return response.data;
};

export const getUserUsageReportFn = async (params: UserUsageFilters): Promise<UserUsageReportResponse> => {
  const response = await privateApi.get<UserUsageReportResponse>('/admin/reports/users', { params });
  return response.data;
};

export const getUserUsageTimelineFn = async (
  ownerId: string,
  params: ReportRangeParams,
): Promise<UserUsageTimelineResponse> => {
  const response = await privateApi.get<UserUsageTimelineResponse>(`/admin/reports/users/${ownerId}/usage`, { params });
  return response.data;
};

export const getAuditEventsFn = async (params: AuditFilters): Promise<AuditEventsResponse> => {
  const response = await privateApi.get<AuditEventsResponse>('/admin/reports/audit-events', { params });
  return response.data;
};

export const refreshUsageSnapshotsFn = async (): Promise<RefreshUsageSnapshotsResponse> => {
  const response = await privateApi.post<RefreshUsageSnapshotsResponse>('/admin/reports/usage-snapshots/refresh');
  return response.data;
};
