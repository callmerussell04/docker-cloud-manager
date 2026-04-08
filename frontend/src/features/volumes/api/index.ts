import { privateApi } from '@/api/axios';
import type { VolumeData } from '../types';

export const getVolumesFn = async (): Promise<VolumeData[]> => {
  const response = await privateApi.get<{ volumes: VolumeData[] }>('/volumes');
  return response.data.volumes || [];
};