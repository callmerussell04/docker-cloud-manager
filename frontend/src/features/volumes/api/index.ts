import { privateApi } from '@/api/axios';
import type { VolumeData, CreateVolumeDTO } from '../types';

export const getVolumesFn = async (): Promise<VolumeData[]> => {
  const response = await privateApi.get<{ volumes: VolumeData[] }>('/volumes');
  return response.data.volumes || [];
};

export const createVolumeFn = async (data: CreateVolumeDTO): Promise<{ volume_id: string }> => {
  const response = await privateApi.post('/volumes', data);
  return response.data;
};

export const deleteVolumeFn = async (id: string): Promise<void> => {
  await privateApi.delete(`/volumes/${id}`);
};