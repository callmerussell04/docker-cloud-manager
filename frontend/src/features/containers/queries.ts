import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { PageParams } from '@/shared/api/pagination';
import type { Scope } from '@/shared/api/scope';
import { queryRoots } from '@/shared/api/queryRoots';
import { useToastStore } from '@/shared/ui/toastStore';
import { getApiErrorMessage, isNotFoundError } from '@/shared/api/apiError';
import { useT, type TranslationKey } from '@/shared/i18n';
import {
  actionContainerFn,
  createContainerFn,
  exposeContainerFn,
  getContainerFn,
  getContainerStatsFn,
  getContainersFn,
} from './api';
import { containerNeedsPolling, containerNeedsWatching, type ContainerAction, type ContainerData } from './model';

// Keys include the scope so that own and all-users data never share a cache
// entry; invalidating containerKeys.all covers both scopes.
export const containerKeys = {
  all: queryRoots.containers,
  lists: (scope: Scope) => [...containerKeys.all, scope, 'list'] as const,
  list: (scope: Scope, params: PageParams & { projectId?: string }) => [...containerKeys.lists(scope), params] as const,
  detail: (scope: Scope, id: string) => [...containerKeys.all, scope, 'detail', id] as const,
  stats: (scope: Scope, id: string) => [...containerKeys.all, scope, 'stats', id] as const,
};

// useContainers polls while a listed container needs it. `polling` also keeps
// it polling when the caller knows the list is about to change, as a project's
// containers do while the project deploys.
export function useContainers<S extends Scope>(scope: S, page: number, limit: number, { projectId, enabled = true, polling = false }: { projectId?: string; enabled?: boolean; polling?: boolean } = {}) {
  return useQuery({
    queryKey: containerKeys.list(scope, { page, limit, projectId }),
    queryFn: () => getContainersFn(scope, page, limit, projectId),
    enabled,
    refetchInterval: (query) => {
      if (!enabled) return false;
      const containers = query.state.data?.items || [];
      return polling || containers.some(containerNeedsPolling) ? 5000 : false;
    },
  });
}

// useContainer reads one container and keeps it current while it is busy or
// running. `snapshot` is shown until the first answer arrives. A container
// that no longer exists is not asked for again.
export function useContainer(scope: Scope, id: string | undefined, snapshot?: ContainerData) {
  return useQuery({
    queryKey: containerKeys.detail(scope, id || ''),
    queryFn: (): Promise<ContainerData> => getContainerFn(scope, id as string),
    enabled: !!id,
    placeholderData: snapshot,
    retry: (failureCount, error) => !isNotFoundError(error) && failureCount < 1,
    refetchInterval: (query) => {
      if (isNotFoundError(query.state.error)) return false;
      const container = query.state.data;
      return !container || containerNeedsWatching(container) ? 5000 : false;
    },
  });
}

// useContainerStats reads live stats only while the container runs; a stopped
// container has none.
export function useContainerStats(scope: Scope, id: string | undefined, running: boolean) {
  return useQuery({
    queryKey: containerKeys.stats(scope, id || ''),
    queryFn: () => getContainerStatsFn(scope, id as string),
    enabled: !!id && running,
    refetchInterval: running ? 3000 : false,
  });
}

export function useCreateContainer() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: createContainerFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: containerKeys.all });
      addToast(t('containers.created'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('containers.createFailed'), t).message, 'error');
    },
  });
}

export function useContainerAction(scope: Scope) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: (variables: { id: string; action: ContainerAction }) => actionContainerFn({ scope, ...variables }),
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: containerKeys.all });
      // A container's state is part of its project's state.
      queryClient.invalidateQueries({ queryKey: queryRoots.projects });
      addToast(scope === 'all'
        ? t('admin.resources.done')
        : t('containers.actionSent', { action: t(`containers.action.${variables.action}` as TranslationKey) }), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t(scope === 'all' ? 'admin.resources.actionFailed' : 'containers.actionFailed'), t).message, 'error');
    },
  });
}

export function useExposeContainer() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: exposeContainerFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: containerKeys.all });
      // A container's state is part of its project's state.
      queryClient.invalidateQueries({ queryKey: queryRoots.projects });
      addToast(t('containers.routingUpdated'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('containers.routingUpdateFailed'), t).message, 'error');
    },
  });
}
