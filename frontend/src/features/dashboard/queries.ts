import { useQuery } from '@tanstack/react-query';
import { queryRoots } from '@/shared/api/queryRoots';
import { getDashboardStatsFn } from './api';

export const dashboardKeys = {
  stats: [...queryRoots.dashboard, 'stats'] as const,
};

export function useDashboardStats(enabled = true) {
  return useQuery({
    queryKey: dashboardKeys.stats,
    queryFn: getDashboardStatsFn,
    enabled,
  });
}
