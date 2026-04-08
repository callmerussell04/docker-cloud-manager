import { privateApi } from '@/api/axios';
import type { ContainerData, CreateContainerDTO, ExposeContainerDTO } from '../types';

export const getContainersFn = async (): Promise<ContainerData[]> => {
  const response = await privateApi.get<{ containers: ContainerData[] }>('/containers');
  return response.data.containers || [];
};

export const createContainerFn = async (data: CreateContainerDTO): Promise<{ container_id: string }> => {
  const response = await privateApi.post('/containers', data);
  return response.data;
};

export const actionContainerFn = async ({ id, action }: { id: string; action: 'start' | 'stop' | 'delete' }): Promise<void> => {
  await privateApi.post(`/containers/${id}/action/${action}`);
};

export const exposeContainerFn = async ({ id, data }: { id: string; data: ExposeContainerDTO }): Promise<void> => {
  await privateApi.post(`/containers/${id}/expose`, data);
};