import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNotificationStore } from '@/shared/ui/notificationStore';
import { failureReason } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import {
  createAdminUserFn,
  deactivateAdminUserFn,
  getAdminUsersFn,
  reactivateAdminUserFn,
  resetAdminUserQuotaFn,
  setAdminUserQuotaFn,
  updateAdminUserFn,
} from './api';
import type { PageParams } from '@/shared/api/pagination';

export const userKeys = {
  all: ['admin', 'users'] as const,
  lists: () => [...userKeys.all, 'list'] as const,
  list: (params: PageParams) => [...userKeys.lists(), params] as const,
};

export function useAdminUsers(page: number, limit: number) {
  return useQuery({
    queryKey: userKeys.list({ page, limit }),
    queryFn: () => getAdminUsersFn(page, limit),
  });
}

export function useCreateAdminUser() {
  const queryClient = useQueryClient();
  const notify = useNotificationStore((state) => state.notify);
  const t = useT();

  return useMutation({
    mutationFn: createAdminUserFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: userKeys.all });
      notify(t('admin.users.created'), 'success');
    },
    onError: (error: unknown) => {
      notify(t('admin.users.createFailed'), 'error', failureReason(error, t));
    },
  });
}

export function useUpdateAdminUser() {
  const queryClient = useQueryClient();
  const notify = useNotificationStore((state) => state.notify);
  const t = useT();

  return useMutation({
    mutationFn: updateAdminUserFn,
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: userKeys.all });
      // An update that deactivates the account also stops its workloads.
      if (!data.workloads) {
        notify(t('admin.users.updated'), 'success');
      } else if (data.workloads.remaining > 0) {
        notify(t('admin.users.deactivatedWorkloadsRemain', { count: data.workloads.remaining }), 'info');
      } else {
        notify(t('admin.users.deactivatedWorkloadsStopped'), 'success');
      }
    },
    onError: (error: unknown) => {
      // The account may be saved as deactivated even though its workloads were not stopped.
      queryClient.invalidateQueries({ queryKey: userKeys.all });
      notify(t('admin.users.updateFailed'), 'error', failureReason(error, t));
    },
  });
}

export function useDeactivateAdminUser() {
  const queryClient = useQueryClient();
  const notify = useNotificationStore((state) => state.notify);
  const t = useT();

  return useMutation({
    mutationFn: deactivateAdminUserFn,
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: userKeys.all });
      if (data.workloads.remaining > 0) {
        notify(t('admin.users.deactivatedWorkloadsRemain', { count: data.workloads.remaining }), 'info');
        return;
      }
      notify(t('admin.users.deactivatedWorkloadsStopped'), 'success');
    },
    onError: (error: unknown) => {
      // The account may be deactivated even though its workloads were not stopped.
      queryClient.invalidateQueries({ queryKey: userKeys.all });
      notify(t('admin.users.deactivateFailed'), 'error', failureReason(error, t));
    },
  });
}

export function useReactivateAdminUser() {
  const queryClient = useQueryClient();
  const notify = useNotificationStore((state) => state.notify);
  const t = useT();

  return useMutation({
    mutationFn: reactivateAdminUserFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: userKeys.all });
      notify(t('admin.users.activated'), 'success');
    },
    onError: (error: unknown) => {
      notify(t('admin.users.activateFailed'), 'error', failureReason(error, t));
    },
  });
}

export function useSetAdminUserQuota() {
  const queryClient = useQueryClient();
  const notify = useNotificationStore((state) => state.notify);
  const t = useT();

  return useMutation({
    mutationFn: setAdminUserQuotaFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: userKeys.all });
      notify(t('admin.users.quotaSaved'), 'success');
    },
    onError: (error: unknown) => {
      notify(t('admin.users.quotaSaveFailed'), 'error', failureReason(error, t));
    },
  });
}

export function useResetAdminUserQuota() {
  const queryClient = useQueryClient();
  const notify = useNotificationStore((state) => state.notify);
  const t = useT();

  return useMutation({
    mutationFn: resetAdminUserQuotaFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: userKeys.all });
      notify(t('admin.users.quotaReset'), 'success');
    },
    onError: (error: unknown) => {
      notify(t('admin.users.quotaResetFailed'), 'error', failureReason(error, t));
    },
  });
}
