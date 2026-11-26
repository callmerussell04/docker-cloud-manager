import { privateApi } from '@/shared/api/client';
import type { PaginatedResponse } from '@/shared/api/pagination';
import { scopedPath, type Scope } from '@/shared/api/scope';
import type { BuildAvailability, BuildOf, CreateBuildGitPayload, ImageOf } from './model';

export const getImagesFn = async <S extends Scope>(scope: S, page = 1, limit = 20): Promise<PaginatedResponse<ImageOf<S>>> => {
  const response = await privateApi.get<{ images: ImageOf<S>[]; total_count: number }>(scopedPath(scope, '/images'), {
    params: { page, limit },
  });
  return { items: response.data.images || [], total_count: response.data.total_count || 0 };
};

export const deleteImageFn = async ({ scope, id }: { scope: Scope; id: string }): Promise<void> => {
  await privateApi.delete(scopedPath(scope, `/images/${id}`));
};

export const getBuildsFn = async <S extends Scope>(scope: S, page = 1, limit = 20): Promise<PaginatedResponse<BuildOf<S>>> => {
  const response = await privateApi.get<{ builds: BuildOf<S>[]; total_count: number }>(scopedPath(scope, '/builds'), {
    params: { page, limit },
  });
  return { items: response.data.builds || [], total_count: response.data.total_count || 0 };
};

export const getBuildFn = async <S extends Scope>(scope: S, id: string): Promise<BuildOf<S>> => {
  const response = await privateApi.get<BuildOf<S>>(scopedPath(scope, `/builds/${id}`));
  return response.data;
};

export const deleteBuildFn = async ({ scope, id }: { scope: Scope; id: string }): Promise<void> => {
  await privateApi.delete(scopedPath(scope, `/builds/${id}`));
};

export const cancelBuildFn = async ({ scope, id }: { scope: Scope; id: string }): Promise<void> => {
  await privateApi.post(scopedPath(scope, `/builds/${id}/cancel`));
};

export const getBuildAvailabilityFn = async (): Promise<BuildAvailability> => {
  const response = await privateApi.get<BuildAvailability>('/images/build/availability');
  return response.data;
};

export const createBuildFn = async (formData: FormData): Promise<{ build_id: string }> => {
  const response = await privateApi.post('/images/build', formData, {
    headers: {
      'Content-Type': 'multipart/form-data',
    },
  });
  return response.data;
};

export const createBuildFromGitFn = async (payload: CreateBuildGitPayload): Promise<{ build_id: string }> => {
  const response = await privateApi.post('/images/build/git', payload);
  return response.data;
};

export const getBuildLogsFn = async (scope: Scope, id: string): Promise<string> => {
  const response = await privateApi.get(scopedPath(scope, `/builds/${id}/logs`), {
    responseType: 'text',
  });
  return response.data;
};
