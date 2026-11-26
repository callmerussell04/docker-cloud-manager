import { useQuery } from '@tanstack/react-query';
import { getSystemMonitoringFn } from './api';
import { vitalsIntervalMs } from './model';

export const monitoringKeys = {
  system: ['admin', 'monitoring'] as const,
};

// useSystemMonitoring reads the host every vitalsIntervalMs while a page
// shows it. The interval belongs to the page's observer, so leaving the page
// stops it, and a hidden tab pauses it.
export function useSystemMonitoring() {
  return useQuery({
    queryKey: monitoringKeys.system,
    queryFn: getSystemMonitoringFn,
    refetchInterval: vitalsIntervalMs,
  });
}
