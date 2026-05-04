import { privateApi } from '@/api/axios';
import { type ProjectData } from '../types';
import type { PaginatedResponse } from '@/features/admin/types';

export const getProjectsFn = async (page = 1, limit = 20): Promise<PaginatedResponse<ProjectData>> => {
  const response = await privateApi.get<{ projects: ProjectData[]; total_count: number }>('/projects', {
    params: { page, limit },
  });
  return { items: response.data.projects || [], total_count: response.data.total_count || 0 };
};

export const createProjectFn = async (formData: FormData): Promise<{ project_id: string }> => {
  const response = await privateApi.post('/projects/compose', formData, {
    headers: {
      'Content-Type': 'multipart/form-data',
    },
  });
  return response.data;
};

export const startProjectFn = async (id: string): Promise<void> => {
  await privateApi.post(`/projects/${id}/start`);
};

export const stopProjectFn = async (id: string): Promise<void> => {
  await privateApi.post(`/projects/${id}/stop`);
};

export const cancelProjectFn = async (id: string): Promise<void> => {
  await privateApi.post(`/projects/${id}/cancel`);
};

export const deleteProjectFn = async (id: string): Promise<void> => {
  await privateApi.delete(`/projects/${id}`);
};
