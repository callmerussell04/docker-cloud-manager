import { privateApi } from '@/api/axios';
import { type DashboardStats } from '../types';

export const getDashboardStatsFn = async (): Promise<DashboardStats> => {
  const response = await privateApi.get<DashboardStats>('/stats');
  return response.data;
};