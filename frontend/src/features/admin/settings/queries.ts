import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useToastStore } from '@/shared/ui/toastStore';
import { getApiErrorMessage } from '@/shared/api/apiError';
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
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: updateSystemConfigFn,
    onSuccess: (_, saved) => {
      // The saved values are the server's values now; showing them at once
      // keeps the form from falling back to the previous read while it reloads.
      queryClient.setQueryData(settingsKeys.systemConfig, saved);
      queryClient.invalidateQueries({ queryKey: settingsKeys.systemConfig });
      addToast(t('admin.settings.updated'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('admin.settings.updateFailed'), t).message, 'error');
    },
  });
}
