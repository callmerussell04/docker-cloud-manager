import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { queryKeys } from '@/shared/api/queryKeys';
import { useToastStore } from '@/store/toastStore';
import { getApiErrorMessage } from '@/lib/apiError';
import { useT, type TranslationKey } from '@/lib/i18n';
import {
  actionContainerFn,
  createContainerFn,
  exposeContainerFn,
  getContainerStatsFn,
  getContainersFn,
  getLogsTicketFn,
  getTerminalTicketFn,
} from './api';
import { adminGetContainerStatsFn } from '@/features/admin/api';
import type { ContainerData, ContainerStats } from './types';

const containerBusyStatuses = ['pending', 'creating', 'starting', 'stopping', 'exposing', 'deleting'];

function shouldPollContainers(containers: ContainerData[]) {
  return containers.some((container) => (
    containerBusyStatuses.includes(container.status) ||
    (container.status === 'running' && !!container.ttl_deadline)
  ));
}

export function useContainers(page: number, limit: number) {
  return useQuery({
    queryKey: queryKeys.containers.list({ page, limit }),
    queryFn: () => getContainersFn(page, limit),
    refetchInterval: (query) => {
      const containers = query.state.data?.items || [];
      return shouldPollContainers(containers) ? 5000 : false;
    },
  });
}

export function useContainerStats(
  id: string | undefined,
  isAdmin: boolean,
  queryFn: () => Promise<ContainerStats>,
  refetchInterval?: number | false,
) {
  return useQuery({
    queryKey: queryKeys.containers.stats(id || '', isAdmin),
    queryFn,
    enabled: !!id,
    refetchInterval,
  });
}

export function useContainerDetailsStats(id: string | undefined, isAdmin: boolean, container?: ContainerData) {
  return useContainerStats(
    id,
    isAdmin,
    () => (isAdmin ? adminGetContainerStatsFn(id as string) : getContainerStatsFn(id as string)),
    !container || container.status === 'running' ? 3000 : false,
  );
}

export function useUserContainerStats(id: string | undefined) {
  return useQuery({
    queryKey: queryKeys.containers.stats(id || '', false),
    queryFn: () => getContainerStatsFn(id as string),
    enabled: !!id,
  });
}

export function useCreateContainer() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: createContainerFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.containers.all });
      addToast(t('containers.created'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('containers.createFailed'), t).message, 'error');
    },
  });
}

export function useContainerAction() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: actionContainerFn,
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.containers.all });
      queryClient.invalidateQueries({ queryKey: queryKeys.admin.containers.all });
      addToast(t('containers.actionSent', { action: t(`containers.action.${variables.action}` as TranslationKey) }), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('containers.actionFailed'), t).message, 'error');
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
      queryClient.invalidateQueries({ queryKey: queryKeys.containers.all });
      addToast(t('containers.routingUpdated'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('containers.routingUpdateFailed'), t).message, 'error');
    },
  });
}

export function useContainerLogsTicket(id: string, isAdmin = false) {
  return getLogsTicketFn(id, isAdmin);
}

export function useContainerTerminalTicket(id: string, isAdmin = false) {
  return getTerminalTicketFn(id, isAdmin);
}
