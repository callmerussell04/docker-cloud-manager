import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { queryKeys } from '@/shared/api/queryKeys';
import { useToastStore } from '@/store/toastStore';
import { getApiErrorMessage } from '@/lib/apiError';
import { useT } from '@/lib/i18n';
import { createVolumeFn, deleteVolumeFn, getVolumesFn } from './api';

export function useVolumes(page: number, limit: number, enabled = true) {
  return useQuery({
    queryKey: queryKeys.volumes.list({ page, limit }),
    queryFn: () => getVolumesFn(page, limit),
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
      queryClient.invalidateQueries({ queryKey: queryKeys.volumes.all });
      addToast(t('volumes.created'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('volumes.createFailed'), t).message, 'error');
    },
  });
}

export function useDeleteVolume() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: deleteVolumeFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.volumes.all });
      addToast(t('volumes.deleted'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('volumes.deleteFailed'), t).message, 'error');
    },
  });
}
