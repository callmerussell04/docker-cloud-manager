import { privateApi } from '@/shared/api/client';
import type { SystemMonitoring } from './model';

export const getSystemMonitoringFn = async (): Promise<SystemMonitoring> => {
  const response = await privateApi.get<SystemMonitoring>('/admin/monitoring');
  return response.data;
};
