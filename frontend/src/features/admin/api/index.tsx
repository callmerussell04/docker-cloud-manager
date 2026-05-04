import { privateApi } from '@/api/axios';
import type { SystemConfig } from '../types';
import type { ContainerData, ContainerStats } from '@/features/containers/types';
import type { VolumeData } from '@/features/volumes/types';
import type { ImageData, BuildData } from '@/features/images/types';
import type { ProjectData } from '@/features/projects/types';

export const getSystemConfigFn = async (): Promise<SystemConfig> => {
  const response = await privateApi.get<SystemConfig>('/admin/config');
  return response.data;
};

export const updateSystemConfigFn = async (data: SystemConfig): Promise<void> => {
  await privateApi.put('/admin/config', data);
};

export const getAllContainersFn = async (page: number, limit: number) => {
  const response = await privateApi.get<{ containers: ContainerData[], total_count: number }>(`/admin/containers?page=${page}&limit=${limit}`);
  return { items: response.data.containers ||[], total_count: response.data.total_count || 0 };
};

export const getAllVolumesFn = async (page: number, limit: number) => {
  const response = await privateApi.get<{ volumes: VolumeData[], total_count: number }>(`/admin/volumes?page=${page}&limit=${limit}`);
  return { items: response.data.volumes ||[], total_count: response.data.total_count || 0 };
};

export const getAllImagesFn = async (page: number, limit: number) => {
  const response = await privateApi.get<{ images: ImageData[], total_count: number }>(`/admin/images?page=${page}&limit=${limit}`);
  return { items: response.data.images ||[], total_count: response.data.total_count || 0 };
};

export const getAllBuildsFn = async (page: number, limit: number) => {
  const response = await privateApi.get<{ builds: BuildData[], total_count: number }>(`/admin/builds?page=${page}&limit=${limit}`);
  return { items: response.data.builds ||[], total_count: response.data.total_count || 0 };
};

export const getAllProjectsFn = async (page: number, limit: number) => {
  const response = await privateApi.get<{ projects: ProjectData[], total_count: number }>(`/admin/projects?page=${page}&limit=${limit}`);
  return { items: response.data.projects ||[], total_count: response.data.total_count || 0 };
};

export const adminActionContainerFn = async ({ id, action }: { id: string; action: 'start' | 'stop' | 'delete' }): Promise<void> => {
  await privateApi.post(`/admin/containers/${id}/action/${action}`);
};

export const adminDeleteVolumeFn = async (id: string): Promise<void> => {
  await privateApi.delete(`/admin/volumes/${id}`);
};

export const adminDeleteImageFn = async (id: string): Promise<void> => {
  await privateApi.delete(`/admin/images/${id}`);
};

export const adminDeleteBuildFn = async (id: string): Promise<void> => {
  await privateApi.delete(`/admin/builds/${id}`);
};

export const adminActionProjectFn = async ({ id, action }: { id: string; action: 'start' | 'stop' | 'delete' }): Promise<void> => {
  if (action === 'delete') {
    await privateApi.delete(`/admin/projects/${id}`);
  } else {
    await privateApi.post(`/admin/projects/${id}/${action}`);
  }
};

export const adminGetContainerStatsFn = async (id: string): Promise<ContainerStats> => {
  const response = await privateApi.get<ContainerStats>(`/admin/containers/${id}/stats`);
  return response.data;
};