import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { PageParams } from '@/shared/api/pagination';
import type { Scope } from '@/shared/api/scope';
import { queryRoots } from '@/shared/api/queryRoots';
import { useNotificationStore } from '@/shared/ui/notificationStore';
import { failureReason, isNotFoundError } from '@/shared/api/apiError';
import { useT, type TranslationKey } from '@/shared/i18n';
import {
  actionContainerFn,
  createContainerFn,
  exposeContainerFn,
  getContainerFn,
  getContainerStatsFn,
  getContainersFn,
} from './api';
import { containerNeedsPolling, containerNeedsWatching, type ContainerAction, type ContainerData, type ContainerListParams } from './model';

// Keys include the scope so that own and all-users data never share a cache
// entry; invalidating containerKeys.all covers both scopes.
export const containerKeys = {
  all: queryRoots.containers,
  lists: (scope: Scope) => [...containerKeys.all, scope, 'list'] as const,
  list: (scope: Scope, params: PageParams & { projectId?: string } & ContainerListParams) => [...containerKeys.lists(scope), params] as const,
  detail: (scope: Scope, id: string) => [...containerKeys.all, scope, 'detail', id] as const,
  stats: (scope: Scope, id: string) => [...containerKeys.all, scope, 'stats', id] as const,
};

// useContainers reads a page of the containers list, filtered, sorted and
// searched on the server by `list`, and polls while a container on the page
// needs it. `polling` also keeps it polling when the caller knows the list is
// about to change, as a project's containers do while the project deploys.
// While another filter, sort or search loads, the last page stays shown.
export function useContainers<S extends Scope>(
  scope: S,
  page: number,
  limit: number,
  { projectId, enabled = true, polling = false, list = {} }: { projectId?: string; enabled?: boolean; polling?: boolean; list?: ContainerListParams } = {},
) {
  return useQuery({
    queryKey: containerKeys.list(scope, { page, limit, projectId, ...list }),
    queryFn: () => getContainersFn(scope, page, limit, projectId, list),
    enabled,
    // Only the same project's list stands in, never another project's.
    placeholderData: (previous, previousQuery) => {
      const params = previousQuery?.queryKey.at(-1) as { projectId?: string } | undefined;
      return params?.projectId === projectId ? keepPreviousData(previous) : undefined;
    },
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
  const notify = useNotificationStore((state) => state.notify);
  const t = useT();

  return useMutation({
    mutationFn: createContainerFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: containerKeys.all });
      // The new container reserves memory against the dashboard's quota.
      queryClient.invalidateQueries({ queryKey: queryRoots.dashboard });
      notify(t('containers.created'), 'success');
    },
    onError: (error: unknown) => {
      notify(t('containers.createFailed'), 'error', failureReason(error, t));
    },
  });
}

export function useContainerAction(scope: Scope) {
  const queryClient = useQueryClient();
  const notify = useNotificationStore((state) => state.notify);
  const t = useT();

  return useMutation({
    mutationFn: (variables: { id: string; action: ContainerAction }) => actionContainerFn({ scope, ...variables }),
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: containerKeys.all });
      // A container's state is part of its project's state.
      queryClient.invalidateQueries({ queryKey: queryRoots.projects });
      notify(scope === 'all'
        ? t('admin.resources.done')
        : t('containers.actionSent', { action: t(`containers.action.${variables.action}` as TranslationKey) }), 'success');
    },
    onError: (error: unknown) => {
      notify(t(scope === 'all' ? 'admin.resources.actionFailed' : 'containers.actionFailed'), 'error', failureReason(error, t));
    },
  });
}

export function useExposeContainer() {
  const queryClient = useQueryClient();
  const notify = useNotificationStore((state) => state.notify);
  const t = useT();

  return useMutation({
    mutationFn: exposeContainerFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: containerKeys.all });
      // A container's state is part of its project's state.
      queryClient.invalidateQueries({ queryKey: queryRoots.projects });
      notify(t('containers.routingUpdated'), 'success');
    },
    onError: (error: unknown) => {
      notify(t('containers.routingUpdateFailed'), 'error', failureReason(error, t));
    },
  });
}
