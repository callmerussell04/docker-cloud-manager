import { privateApi } from '@/shared/api/client';
import type { PaginatedResponse } from '@/shared/api/pagination';
import { scopedPath, type Scope } from '@/shared/api/scope';
import type { CreateProjectGitPayload, ProjectAction, ProjectOf } from './model';

export const getProjectsFn = async <S extends Scope>(scope: S, page = 1, limit = 20): Promise<PaginatedResponse<ProjectOf<S>>> => {
  const response = await privateApi.get<{ projects: ProjectOf<S>[]; total_count: number }>(scopedPath(scope, '/projects'), {
    params: { page, limit },
  });
  return { items: response.data.projects || [], total_count: response.data.total_count || 0 };
};

export const getProjectFn = async <S extends Scope>(scope: S, id: string): Promise<ProjectOf<S>> => {
  const response = await privateApi.get<ProjectOf<S>>(scopedPath(scope, `/projects/${id}`));
  return response.data;
};

export const createProjectFn = async (formData: FormData): Promise<{ project_id: string }> => {
  const response = await privateApi.post('/projects/compose', formData, {
    headers: {
      'Content-Type': 'multipart/form-data',
    },
  });
  return response.data;
};

export const createProjectFromGitFn = async (payload: CreateProjectGitPayload): Promise<{ project_id: string }> => {
  const response = await privateApi.post('/projects/compose/git', payload);
  return response.data;
};

export const actionProjectFn = async ({ scope, id, action }: { scope: Scope; id: string; action: ProjectAction }): Promise<void> => {
  if (action === 'delete') {
    await privateApi.delete(scopedPath(scope, `/projects/${id}`));
  } else {
    await privateApi.post(scopedPath(scope, `/projects/${id}/${action}`));
  }
};
