import { privateApi } from '@/api/axios';
import type { ImageData, BuildData } from '../types';

export const getImagesFn = async (): Promise<ImageData[]> => {
  const response = await privateApi.get<{ images: ImageData[] }>('/images');
  return response.data.images || [];
};

export const deleteImageFn = async (id: string): Promise<void> => {
  await privateApi.delete(`/images/${id}`);
};

export const getBuildsFn = async (): Promise<BuildData[]> => {
  const response = await privateApi.get<{ builds: BuildData[] }>('/builds');
  return response.data.builds || [];
};

export const deleteBuildFn = async (id: string): Promise<void> => {
  await privateApi.delete(`/builds/${id}`);
};

export const createBuildFn = async (formData: FormData): Promise<{ build_id: string }> => {
  const response = await privateApi.post('/images/build', formData, {
    headers: {
      'Content-Type': 'multipart/form-data',
    },
  });
  return response.data;
};

export const getBuildLogsFn = async (id: string): Promise<string> => {
  const response = await privateApi.get(`/builds/${id}/logs`, {
    responseType: 'text',
  });
  return response.data;
};