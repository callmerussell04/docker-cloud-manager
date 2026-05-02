import { privateApi } from '@/api/axios';
import type { ContainerData, CreateContainerDTO, ExposeContainerDTO, ContainerStats, TelemetryTicketResponse } from '../types';

export const getContainersFn = async (): Promise<ContainerData[]> => {
  const response = await privateApi.get<{ containers: ContainerData[] }>('/containers');
  return response.data.containers ||[];
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

export const getContainerStatsFn = async (id: string): Promise<ContainerStats> => {
  const response = await privateApi.get<ContainerStats>(`/containers/${id}/stats`);
  return response.data;
};

export const getLogsTicketFn = async (id: string, isAdmin = false): Promise<TelemetryTicketResponse> => {
  const prefix = isAdmin ? '/admin' : '';
  const response = await privateApi.post(`${prefix}/containers/${id}/logs/stream/ticket`);
  return response.data;
};

export const getTerminalTicketFn = async (id: string, isAdmin = false): Promise<TelemetryTicketResponse> => {
  const prefix = isAdmin ? '/admin' : '';
  const response = await privateApi.post(`${prefix}/containers/${id}/terminal/ticket`);
  return response.data;
};