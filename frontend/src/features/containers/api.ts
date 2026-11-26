import { privateApi } from '@/shared/api/client';
import { scopedPath, type Scope } from '@/shared/api/scope';
import type {
  ContainerAction,
  ContainerListParams,
  ContainerOf,
  ContainerPage,
  ContainerStats,
  ContainerStatusCounts,
  CreateContainerDTO,
  ExposeContainerDTO,
  TelemetryTicketResponse,
} from './model';

export const getContainersFn = async <S extends Scope>(
  scope: S,
  page = 1,
  limit = 20,
  projectId?: string,
  list: ContainerListParams = {},
): Promise<ContainerPage<ContainerOf<S>>> => {
  const response = await privateApi.get<{ containers: ContainerOf<S>[]; total_count: number; status_counts: ContainerStatusCounts }>(scopedPath(scope, '/containers'), {
    params: { page, limit, project_id: projectId || undefined, ...list },
  });
  return { items: response.data.containers || [], total_count: response.data.total_count || 0, status_counts: response.data.status_counts };
};

export const getContainerFn = async <S extends Scope>(scope: S, id: string): Promise<ContainerOf<S>> => {
  const response = await privateApi.get<ContainerOf<S>>(scopedPath(scope, `/containers/${id}`));
  return response.data;
};

export const createContainerFn = async (data: CreateContainerDTO): Promise<{ container_id: string }> => {
  const response = await privateApi.post('/containers', data);
  return response.data;
};

export const actionContainerFn = async ({ scope, id, action }: { scope: Scope; id: string; action: ContainerAction }): Promise<void> => {
  await privateApi.post(scopedPath(scope, `/containers/${id}/action/${action}`));
};

export const exposeContainerFn = async ({ id, data }: { id: string; data: ExposeContainerDTO }): Promise<void> => {
  await privateApi.post(`/containers/${id}/expose`, data);
};

export const getContainerStatsFn = async (scope: Scope, id: string): Promise<ContainerStats> => {
  const response = await privateApi.get<ContainerStats>(scopedPath(scope, `/containers/${id}/stats`));
  return response.data;
};

// containerStreamPath is the path of a container's log or terminal stream,
// which a one-time ticket from the matching ticket endpoint opens.
export const containerStreamPath = (scope: Scope, id: string, stream: 'logs' | 'terminal') => (
  scopedPath(scope, stream === 'logs' ? `/containers/${id}/logs/stream` : `/containers/${id}/terminal`)
);

export const getLogsTicketFn = async (scope: Scope, id: string): Promise<TelemetryTicketResponse> => {
  const response = await privateApi.post(scopedPath(scope, `/containers/${id}/logs/stream/ticket`));
  return response.data;
};

export const getTerminalTicketFn = async (scope: Scope, id: string): Promise<TelemetryTicketResponse> => {
  const response = await privateApi.post(scopedPath(scope, `/containers/${id}/terminal/ticket`));
  return response.data;
};
