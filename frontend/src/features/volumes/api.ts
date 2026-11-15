import { privateApi } from '@/shared/api/client';
import type { PaginatedResponse } from '@/shared/api/pagination';
import { scopedPath, type Scope } from '@/shared/api/scope';
import type { CreateVolumeDTO, VolumeOf } from './model';

export const getVolumesFn = async <S extends Scope>(scope: S, page = 1, limit = 20): Promise<PaginatedResponse<VolumeOf<S>>> => {
  const response = await privateApi.get<{ volumes: VolumeOf<S>[]; total_count: number }>(scopedPath(scope, '/volumes'), {
    params: { page, limit },
  });
  return { items: response.data.volumes || [], total_count: response.data.total_count || 0 };
};

export const createVolumeFn = async (data: CreateVolumeDTO): Promise<{ volume_id: string }> => {
  const response = await privateApi.post('/volumes', data);
  return response.data;
};

export const deleteVolumeFn = async ({ scope, id }: { scope: Scope; id: string }): Promise<void> => {
  await privateApi.delete(scopedPath(scope, `/volumes/${id}`));
};
