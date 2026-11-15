import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { PageParams } from '@/shared/api/pagination';
import type { Scope } from '@/shared/api/scope';
import { useToastStore } from '@/shared/ui/toastStore';
import { getApiErrorMessage } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import { createVolumeFn, deleteVolumeFn, getVolumesFn } from './api';

export const volumeKeys = {
  all: ['volumes'] as const,
  lists: (scope: Scope) => [...volumeKeys.all, scope, 'list'] as const,
  list: (scope: Scope, params: PageParams) => [...volumeKeys.lists(scope), params] as const,
};

export function useVolumes<S extends Scope>(scope: S, page: number, limit: number, enabled = true) {
  return useQuery({
    queryKey: volumeKeys.list(scope, { page, limit }),
    queryFn: () => getVolumesFn(scope, page, limit),
    enabled,
  });
}

export function useCreateVolume() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: createVolumeFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: volumeKeys.all });
      addToast(t('volumes.created'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('volumes.createFailed'), t).message, 'error');
    },
  });
}

export function useDeleteVolume(scope: Scope) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: (id: string) => deleteVolumeFn({ scope, id }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: volumeKeys.all });
      addToast(t(scope === 'all' ? 'admin.resources.volumeDeleted' : 'volumes.deleted'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('volumes.deleteFailed'), t).message, 'error');
    },
  });
}
