import { useQuery } from '@tanstack/react-query';
import { getSystemMonitoringFn } from './api';

export const monitoringKeys = {
  system: ['admin', 'monitoring'] as const,
};

export function useSystemMonitoring() {
  return useQuery({
    queryKey: monitoringKeys.system,
    queryFn: getSystemMonitoringFn,
    refetchInterval: 5000,
  });
}
