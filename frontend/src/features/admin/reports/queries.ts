import { keepPreviousData, useMutation, useQueries, useQuery } from '@tanstack/react-query';
import { getAuditEventsFn, getReportsOverviewFn, getUserUsageReportFn, getUserUsageTimelineFn, refreshUsageSnapshotsFn } from './api';
import type { AuditFilters, ReportRangeParams, UserTimeline, UserUsageFilters } from './model';

export const reportKeys = {
  all: ['admin', 'reports'] as const,
  overview: (params: ReportRangeParams) => [...reportKeys.all, 'overview', params] as const,
  users: (params: UserUsageFilters) => [...reportKeys.all, 'users', params] as const,
  userTimeline: (ownerId: string, params: ReportRangeParams) => [...reportKeys.all, 'users', ownerId, 'timeline', params] as const,
  audit: (params: AuditFilters) => [...reportKeys.all, 'audit', params] as const,
};

export function useReportsOverview(params: ReportRangeParams, enabled = true) {
  return useQuery({
    queryKey: reportKeys.overview(params),
    queryFn: () => getReportsOverviewFn(params),
    placeholderData: keepPreviousData,
    enabled,
  });
}

export function useUserUsageReport(params: UserUsageFilters, enabled = true) {
  return useQuery({
    queryKey: reportKeys.users(params),
    queryFn: () => getUserUsageReportFn(params),
    placeholderData: keepPreviousData,
    enabled,
  });
}

// useUserUsageTimelines reads every given user's timeline over the range,
// for the stacked chart and the users table's trends. Each keeps its last
// answer on screen while a moved window reloads it. timelines holds those
// that have answered; failed is whether any could not be read.
export function useUserUsageTimelines(ownerIds: readonly string[], params: ReportRangeParams, enabled = true) {
  return useQueries({
    queries: ownerIds.map((ownerId) => ({
      queryKey: reportKeys.userTimeline(ownerId, params),
      queryFn: () => getUserUsageTimelineFn(ownerId, params),
      placeholderData: keepPreviousData,
      enabled,
    })),
    combine: (results) => ({
      timelines: results.flatMap((result, index): UserTimeline[] =>
        result.data ? [{ ownerId: ownerIds[index], points: result.data.points }] : [],
      ),
      isLoading: results.some((result) => result.isLoading),
      failed: results.some((result) => result.isError),
      refetch: () => results.forEach((result) => void result.refetch()),
    }),
  });
}

export function useAuditEvents(params: AuditFilters, enabled = true) {
  return useQuery({
    queryKey: reportKeys.audit(params),
    queryFn: () => getAuditEventsFn(params),
    placeholderData: keepPreviousData,
    enabled,
  });
}

// The reports page moves its time window after a refresh, which reloads every
// report; nothing is invalidated here.
export function useRefreshUsageSnapshots() {
  return useMutation({ mutationFn: refreshUsageSnapshotsFn });
}
