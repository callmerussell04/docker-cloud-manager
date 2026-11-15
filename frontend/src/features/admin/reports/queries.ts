import { keepPreviousData, useMutation, useQuery } from '@tanstack/react-query';
import { getAuditEventsFn, getReportsOverviewFn, getUserUsageReportFn, getUserUsageTimelineFn, refreshUsageSnapshotsFn } from './api';
import type { AuditFilters, ReportRangeParams, UserUsageFilters } from './model';

export const reportKeys = {
  all: ['admin', 'reports'] as const,
  overview: (params: ReportRangeParams) => [...reportKeys.all, 'overview', params] as const,
  users: (params: UserUsageFilters) => [...reportKeys.all, 'users', params] as const,
  userTimeline: (ownerId: string, params: ReportRangeParams) => [...reportKeys.all, 'users', ownerId, 'timeline', params] as const,
  audit: (params: AuditFilters) => [...reportKeys.all, 'audit', params] as const,
};

export function useReportsOverview(params: ReportRangeParams) {
  return useQuery({
    queryKey: reportKeys.overview(params),
    queryFn: () => getReportsOverviewFn(params),
    placeholderData: keepPreviousData,
  });
}

export function useUserUsageReport(params: UserUsageFilters) {
  return useQuery({
    queryKey: reportKeys.users(params),
    queryFn: () => getUserUsageReportFn(params),
    placeholderData: keepPreviousData,
  });
}

export function useUserUsageTimeline(ownerId: string | undefined, params: ReportRangeParams, enabled = true) {
  return useQuery({
    queryKey: reportKeys.userTimeline(ownerId ?? '', params),
    queryFn: () => getUserUsageTimelineFn(ownerId ?? '', params),
    enabled: enabled && Boolean(ownerId),
  });
}

export function useAuditEvents(params: AuditFilters) {
  return useQuery({
    queryKey: reportKeys.audit(params),
    queryFn: () => getAuditEventsFn(params),
    placeholderData: keepPreviousData,
  });
}

// The reports page moves its time window after a refresh, which reloads every
// report; nothing is invalidated here.
export function useRefreshUsageSnapshots() {
  return useMutation({ mutationFn: refreshUsageSnapshotsFn });
}
