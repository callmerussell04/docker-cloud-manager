import { privateApi } from '@/api/axios';
import type { ImageData, BuildData, BuildAvailability } from '../types';
import type { PaginatedResponse } from '@/features/admin/types';

export const getImagesFn = async (page = 1, limit = 20): Promise<PaginatedResponse<ImageData>> => {
  const response = await privateApi.get<{ images: ImageData[]; total_count: number }>('/images', {
    params: { page, limit },
  });
  return { items: response.data.images || [], total_count: response.data.total_count || 0 };
};

export const deleteImageFn = async (id: string): Promise<void> => {
  await privateApi.delete(`/images/${id}`);
};

export const getBuildsFn = async (page = 1, limit = 20): Promise<PaginatedResponse<BuildData>> => {
  const response = await privateApi.get<{ builds: BuildData[]; total_count: number }>('/builds', {
    params: { page, limit },
  });
  return { items: response.data.builds || [], total_count: response.data.total_count || 0 };
};

export const deleteBuildFn = async (id: string): Promise<void> => {
  await privateApi.delete(`/builds/${id}`);
};

export const cancelBuildFn = async (id: string): Promise<void> => {
  await privateApi.post(`/builds/${id}/cancel`);
};

export const getBuildAvailabilityFn = async (): Promise<BuildAvailability> => {
  const response = await privateApi.get<BuildAvailability>('/images/build/availability');
  return response.data;
};

export const createBuildFn = async (formData: FormData): Promise<{ build_id: string }> => {
  const response = await privateApi.post('/images/build', formData, {
    headers: {
      'Content-Type': 'multipart/form-data',
    },
  });
  return response.data;
};

export const getBuildLogsFn = async (id: string, isAdmin = false): Promise<string> => {
  const prefix = isAdmin ? '/admin' : '';
  const response = await privateApi.get(`${prefix}/builds/${id}/logs`, {
    responseType: 'text',
  });
  return response.data;
};
