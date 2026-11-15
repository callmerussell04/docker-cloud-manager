import { useMutation, useQuery, useQueryClient, type QueryKey } from '@tanstack/react-query';
import type { PageParams } from '@/shared/api/pagination';
import type { Scope } from '@/shared/api/scope';
import { useToastStore } from '@/shared/ui/toastStore';
import { getApiErrorMessage } from '@/shared/api/apiError';
import { useT, type TranslationKey } from '@/shared/i18n';
import {
  actionContainerFn,
  createContainerFn,
  exposeContainerFn,
  getContainerStatsFn,
  getContainersFn,
} from './api';
import type { ContainerAction, ContainerData } from './model';

// Keys include the scope so that own and all-users data never share a cache
// entry; invalidating containerKeys.all covers both scopes.
export const containerKeys = {
  all: ['containers'] as const,
  lists: (scope: Scope) => [...containerKeys.all, scope, 'list'] as const,
  list: (scope: Scope, params: PageParams & { projectId?: string }) => [...containerKeys.lists(scope), params] as const,
  stats: (scope: Scope, id: string) => [...containerKeys.all, scope, 'stats', id] as const,
};

// Mutation options for a feature that shows data derived from containers. The
// projects feature passes its keys here, because containers may not import it.
export interface ContainerMutationOptions {
  alsoInvalidate?: readonly QueryKey[];
}

const containerBusyStatuses = ['pending', 'creating', 'starting', 'stopping', 'exposing', 'deleting'];

function shouldPollContainers(containers: ContainerData[]) {
  return containers.some((container) => (
    containerBusyStatuses.includes(container.status) ||
    (container.status === 'running' && !!container.ttl_deadline)
  ));
}

export function useContainers<S extends Scope>(scope: S, page: number, limit: number, { projectId, enabled = true }: { projectId?: string; enabled?: boolean } = {}) {
  return useQuery({
    queryKey: containerKeys.list(scope, { page, limit, projectId }),
    queryFn: () => getContainersFn(scope, page, limit, projectId),
    enabled,
    refetchInterval: (query) => {
      const containers = query.state.data?.items || [];
      return enabled && shouldPollContainers(containers) ? 5000 : false;
    },
  });
}

export function useContainerStats(scope: Scope, id: string | undefined, container?: ContainerData) {
  return useQuery({
    queryKey: containerKeys.stats(scope, id || ''),
    queryFn: () => getContainerStatsFn(scope, id as string),
    enabled: !!id,
    refetchInterval: !container || container.status === 'running' ? 3000 : false,
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

export function useContainerAction(scope: Scope, { alsoInvalidate = [] }: ContainerMutationOptions = {}) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: (variables: { id: string; action: ContainerAction }) => actionContainerFn({ scope, ...variables }),
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: containerKeys.all });
      alsoInvalidate.forEach((queryKey) => queryClient.invalidateQueries({ queryKey }));
      addToast(scope === 'all'
        ? t('admin.resources.done')
        : t('containers.actionSent', { action: t(`containers.action.${variables.action}` as TranslationKey) }), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t(scope === 'all' ? 'admin.resources.actionFailed' : 'containers.actionFailed'), t).message, 'error');
    },
  });
}

export function useExposeContainer({ alsoInvalidate = [] }: ContainerMutationOptions = {}) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: exposeContainerFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: containerKeys.all });
      alsoInvalidate.forEach((queryKey) => queryClient.invalidateQueries({ queryKey }));
      addToast(t('containers.routingUpdated'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('containers.routingUpdateFailed'), t).message, 'error');
    },
  });
}
