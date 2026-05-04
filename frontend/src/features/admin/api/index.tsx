import { privateApi } from '@/api/axios';
import type { AdminUser, AdminUserForm, SystemConfig } from '../types';
import type { AdminContainerData, ContainerStats } from '@/features/containers/types';
import type { AdminVolumeData } from '@/features/volumes/types';
import type { AdminImageData, AdminBuildData } from '@/features/images/types';
import type { AdminProjectData } from '@/features/projects/types';

export const getSystemConfigFn = async (): Promise<SystemConfig> => {
  const response = await privateApi.get<SystemConfig>('/admin/config');
  return response.data;
};

export const updateSystemConfigFn = async (data: SystemConfig): Promise<void> => {
  await privateApi.put('/admin/config', data);
};

export const getAllContainersFn = async (page: number, limit: number) => {
  const response = await privateApi.get<{ containers: AdminContainerData[], total_count: number }>(`/admin/containers?page=${page}&limit=${limit}`);
  return { items: response.data.containers ||[], total_count: response.data.total_count || 0 };
};

export const getAllVolumesFn = async (page: number, limit: number) => {
  const response = await privateApi.get<{ volumes: AdminVolumeData[], total_count: number }>(`/admin/volumes?page=${page}&limit=${limit}`);
  return { items: response.data.volumes ||[], total_count: response.data.total_count || 0 };
};

export const getAllImagesFn = async (page: number, limit: number) => {
  const response = await privateApi.get<{ images: AdminImageData[], total_count: number }>(`/admin/images?page=${page}&limit=${limit}`);
  return { items: response.data.images ||[], total_count: response.data.total_count || 0 };
};

export const getAllBuildsFn = async (page: number, limit: number) => {
  const response = await privateApi.get<{ builds: AdminBuildData[], total_count: number }>(`/admin/builds?page=${page}&limit=${limit}`);
  return { items: response.data.builds ||[], total_count: response.data.total_count || 0 };
};

export const getAllProjectsFn = async (page: number, limit: number) => {
  const response = await privateApi.get<{ projects: AdminProjectData[], total_count: number }>(`/admin/projects?page=${page}&limit=${limit}`);
  return { items: response.data.projects ||[], total_count: response.data.total_count || 0 };
};

export const getAdminUsersFn = async (page: number, limit: number) => {
  const response = await privateApi.get<{ users: AdminUser[], total_count: number }>(`/admin/users?page=${page}&limit=${limit}`);
  return { items: response.data.users || [], total_count: response.data.total_count || 0 };
};

export const createAdminUserFn = async (data: AdminUserForm): Promise<AdminUser> => {
  const response = await privateApi.post<AdminUser>('/admin/users', data);
  return response.data;
};

export const updateAdminUserFn = async ({ id, data }: { id: string; data: AdminUserForm }): Promise<AdminUser> => {
  const response = await privateApi.put<AdminUser>(`/admin/users/${id}`, data);
  return response.data;
};

export const deactivateAdminUserFn = async (id: string): Promise<AdminUser> => {
  const response = await privateApi.delete<AdminUser>(`/admin/users/${id}`);
  return response.data;
};

export const reactivateAdminUserFn = async (id: string): Promise<AdminUser> => {
  const response = await privateApi.post<AdminUser>(`/admin/users/${id}/activate`);
  return response.data;
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

export const adminCancelBuildFn = async (id: string): Promise<void> => {
  await privateApi.post(`/admin/builds/${id}/cancel`);
};

export const adminActionProjectFn = async ({ id, action }: { id: string; action: 'start' | 'stop' | 'cancel' | 'delete' }): Promise<void> => {
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
