import { useQuery } from '@tanstack/react-query';
import { queryRoots } from '@/shared/api/queryRoots';
import { getDashboardStatsFn } from './api';

export const dashboardKeys = {
  stats: [...queryRoots.dashboard, 'stats'] as const,
};

// useDashboardStats reads the user's figures; `polling` keeps them current
// every 5 s, as a list showing containers that are changing does.
export function useDashboardStats(enabled = true, polling = false) {
  return useQuery({
    queryKey: dashboardKeys.stats,
    queryFn: getDashboardStatsFn,
    enabled,
    refetchInterval: enabled && polling ? 5000 : false,
  });
}
