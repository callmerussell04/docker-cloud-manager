import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNotificationStore } from '@/shared/ui/notificationStore';
import { failureReason } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import { getSystemConfigFn, updateSystemConfigFn } from './api';

export const settingsKeys = {
  systemConfig: ['admin', 'systemConfig'] as const,
};

export function useSystemConfig() {
  return useQuery({
    queryKey: settingsKeys.systemConfig,
    queryFn: getSystemConfigFn,
  });
}

export function useUpdateSystemConfig() {
  const queryClient = useQueryClient();
  const notify = useNotificationStore((state) => state.notify);
  const t = useT();

  return useMutation({
    mutationFn: updateSystemConfigFn,
    onSuccess: (_, saved) => {
      // The saved values are the server's values now; showing them at once
      // keeps the form from falling back to the previous read while it reloads.
      queryClient.setQueryData(settingsKeys.systemConfig, saved);
      queryClient.invalidateQueries({ queryKey: settingsKeys.systemConfig });
      notify(t('admin.settings.updated'), 'success');
    },
    onError: (error: unknown) => {
      notify(t('admin.settings.updateFailed'), 'error', failureReason(error, t));
    },
  });
}
