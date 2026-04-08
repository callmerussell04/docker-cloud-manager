import { privateApi } from '@/api/axios';
import { type ProjectData } from '../types';

export const getProjectsFn = async (): Promise<ProjectData[]> => {
  const response = await privateApi.get<{ projects: ProjectData[] }>('/projects');
  return response.data.projects || [];
};

export const createProjectFn = async (formData: FormData): Promise<{ project_id: string }> => {
  const response = await privateApi.post('/projects/compose', formData, {
    headers: {
      'Content-Type': 'multipart/form-data',
    },
  });
  return response.data;
};

export const stopProjectFn = async (id: string): Promise<void> => {
  await privateApi.post(`/projects/${id}/stop`);
};

export const deleteProjectFn = async (id: string): Promise<void> => {
  await privateApi.delete(`/projects/${id}`);
};