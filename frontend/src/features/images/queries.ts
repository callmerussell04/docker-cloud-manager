import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { PageParams } from '@/shared/api/pagination';
import type { Scope } from '@/shared/api/scope';
import { useToastStore } from '@/shared/ui/toastStore';
import { getApiErrorMessage } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
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
import { isBuildActive, isImageBusy, type CreateBuildGitPayload } from './model';

export const imageKeys = {
  all: ['images'] as const,
  lists: (scope: Scope) => [...imageKeys.all, scope, 'list'] as const,
  list: (scope: Scope, params: PageParams) => [...imageKeys.lists(scope), params] as const,
  availability: ['images', 'buildAvailability'] as const,
};

export const buildKeys = {
  all: ['builds'] as const,
  lists: (scope: Scope) => [...buildKeys.all, scope, 'list'] as const,
  list: (scope: Scope, params: PageParams) => [...buildKeys.lists(scope), params] as const,
  logs: (scope: Scope, id: string | undefined) => [...buildKeys.all, scope, 'logs', id] as const,
};

export function useImages<S extends Scope>(scope: S, page: number, limit: number, enabled = true) {
  return useQuery({
    queryKey: imageKeys.list(scope, { page, limit }),
    queryFn: () => getImagesFn(scope, page, limit),
    enabled,
    refetchInterval: (query) => {
      const images = query.state.data?.items || [];
      return enabled && images.some((image) => isImageBusy(image.status)) ? 5000 : false;
    },
  });
}

// useBuilds polls while a build is pending or running, unless polling is off.
export function useBuilds<S extends Scope>(scope: S, page: number, limit: number, { enabled = true, polling = true }: { enabled?: boolean; polling?: boolean } = {}) {
  return useQuery({
    queryKey: buildKeys.list(scope, { page, limit }),
    queryFn: () => getBuildsFn(scope, page, limit),
    enabled,
    refetchInterval: (query) => {
      if (!enabled || !polling) return false;
      const builds = query.state.data?.items || [];
      return builds.some((build) => isBuildActive(build.status)) ? 5000 : false;
    },
  });
}

export function useBuildAvailability(enabled = true) {
  return useQuery({
    queryKey: imageKeys.availability,
    queryFn: getBuildAvailabilityFn,
    enabled,
  });
}

export function useBuildLogs(scope: Scope, id: string | undefined, enabled: boolean) {
  return useQuery({
    queryKey: buildKeys.logs(scope, id),
    queryFn: () => getBuildLogsFn(scope, id as string),
    enabled,
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
      queryClient.invalidateQueries({ queryKey: buildKeys.all });
      // A build has an image record from the moment it is queued.
      queryClient.invalidateQueries({ queryKey: imageKeys.all });
      addToast(t('images.buildStarted'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('images.buildStartFailed'), t).message, 'error');
    },
  });
}

export function useDeleteImage(scope: Scope) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: (id: string) => deleteImageFn({ scope, id }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: imageKeys.all });
      addToast(t(scope === 'all' ? 'admin.resources.imageDeleted' : 'images.deleted'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('images.deleteFailed'), t).message, 'error');
    },
  });
}

export function useDeleteBuild(scope: Scope) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: (id: string) => deleteBuildFn({ scope, id }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: buildKeys.all });
      // A build has an image record from the moment it is queued.
      queryClient.invalidateQueries({ queryKey: imageKeys.all });
      addToast(t(scope === 'all' ? 'admin.resources.buildDeleted' : 'images.buildRecordDeleted'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('images.deleteBuildRecordFailed'), t).message, 'error');
    },
  });
}

export function useCancelBuild(scope: Scope) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: (id: string) => cancelBuildFn({ scope, id }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: buildKeys.all });
      // A build has an image record from the moment it is queued.
      queryClient.invalidateQueries({ queryKey: imageKeys.all });
      addToast(t('images.cancelBuildRequested'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('images.cancelBuildFailed'), t).message, 'error');
    },
  });
}
