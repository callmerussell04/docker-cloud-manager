import { useQuery } from '@tanstack/react-query';
import { getAuditEventsFn, getReportsOverviewFn, getUserUsageReportFn, getUserUsageTimelineFn } from './api';
import type { AuditFilters, ReportRangeParams } from './types';
import { queryKeys } from '@/shared/api/queryKeys';

export function useReportsOverview(params: ReportRangeParams) {
  return useQuery({
    queryKey: queryKeys.admin.reports.overview(params),
    queryFn: () => getReportsOverviewFn(params),
  });
}

export function useUserUsageReport(params: ReportRangeParams & { sort: string; page: number; limit: number }) {
  return useQuery({
    queryKey: queryKeys.admin.reports.users(params),
    queryFn: () => getUserUsageReportFn(params),
  });
}

export function useUserUsageTimeline(ownerId: string | undefined, params: ReportRangeParams) {
  return useQuery({
    queryKey: queryKeys.admin.reports.userTimeline(ownerId ?? '', params),
    queryFn: () => getUserUsageTimelineFn(ownerId ?? '', params),
    enabled: Boolean(ownerId),
  });
}

export function useAuditEvents(params: AuditFilters) {
  return useQuery({
    queryKey: queryKeys.admin.reports.audit(params),
    queryFn: () => getAuditEventsFn(params),
  });
}
