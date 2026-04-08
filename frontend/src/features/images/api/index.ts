import { privateApi } from '@/api/axios';
import type { ImageData } from '../types';

export const getImagesFn = async (): Promise<ImageData[]> => {
  const response = await privateApi.get<{ images: ImageData[] }>('/images');
  return response.data.images || [];
};