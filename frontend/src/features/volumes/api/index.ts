import { privateApi } from '@/api/axios';
import type { VolumeData, CreateVolumeDTO } from '../types';
import type { PaginatedResponse } from '@/features/admin/types';

export const getVolumesFn = async (page = 1, limit = 20): Promise<PaginatedResponse<VolumeData>> => {
  const response = await privateApi.get<{ volumes: VolumeData[]; total_count: number }>('/volumes', {
    params: { page, limit },
  });
  return { items: response.data.volumes || [], total_count: response.data.total_count || 0 };
};

export const createVolumeFn = async (data: CreateVolumeDTO): Promise<{ volume_id: string }> => {
  const response = await privateApi.post('/volumes', data);
  return response.data;
};

export const deleteVolumeFn = async (id: string): Promise<void> => {
  await privateApi.delete(`/volumes/${id}`);
};
