import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { queryKeys } from '@/shared/api/queryKeys';
import { useToastStore } from '@/store/toastStore';
import { getApiErrorMessage } from '@/lib/apiError';
import { useT } from '@/lib/i18n';
import {
  cancelBuildFn,
  createBuildFn,
  createBuildFromGitFn,
  deleteBuildFn,
  deleteImageFn,
  getBuildAvailabilityFn,
  getBuildLogsFn,
  getBuildsFn,
  getImagesFn,
} from './api';
import type { CreateBuildGitPayload } from './types';

const activeBuildStatuses = new Set(['pending', 'running']);

export function useImages(page: number, limit: number, enabled = true) {
  return useQuery({
    queryKey: queryKeys.images.list({ page, limit }),
    queryFn: () => getImagesFn(page, limit),
    enabled,
  });
}

export function useBuilds(page: number, limit: number, pollingEnabled: boolean) {
  return useQuery({
    queryKey: queryKeys.builds.list({ page, limit }),
    queryFn: () => getBuildsFn(page, limit),
    refetchInterval: (query) => {
      if (!pollingEnabled) return false;
      const builds = query.state.data?.items || [];
      return builds.some((build) => activeBuildStatuses.has(build.status)) ? 5000 : false;
    },
  });
}

export function useBuildAvailability(enabled = true) {
  return useQuery({
    queryKey: queryKeys.images.availability,
    queryFn: getBuildAvailabilityFn,
    enabled,
  });
}

export function useBuildLogs(id: string | undefined, isAdmin: boolean, enabled: boolean, shouldPoll = false) {
  return useQuery({
    queryKey: queryKeys.builds.logs(id, isAdmin),
    queryFn: () => getBuildLogsFn(id as string, isAdmin),
    enabled,
    refetchInterval: shouldPoll ? 3000 : false,
    retry: false,
  });
}

export function useCreateBuild() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: (payload: FormData | CreateBuildGitPayload) => (
      payload instanceof FormData ? createBuildFn(payload) : createBuildFromGitFn(payload)
    ),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.builds.all });
      addToast(t('images.buildStarted'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('images.buildStartFailed'), t).message, 'error');
    },
  });
}

export function useDeleteImage() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: deleteImageFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.images.all });
      addToast(t('images.deleted'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('images.deleteFailed'), t).message, 'error');
    },
  });
}

export function useDeleteBuild() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: deleteBuildFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.builds.all });
      addToast(t('images.buildRecordDeleted'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('images.deleteBuildRecordFailed'), t).message, 'error');
    },
  });
}

export function useCancelBuild() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: cancelBuildFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.builds.all });
      addToast(t('images.cancelBuildRequested'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('images.cancelBuildFailed'), t).message, 'error');
    },
  });
}
