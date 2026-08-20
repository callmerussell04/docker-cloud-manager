import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { queryKeys } from '@/shared/api/queryKeys';
import { useToastStore } from '@/store/toastStore';
import { getApiErrorMessage } from '@/lib/apiError';
import { useT } from '@/lib/i18n';
import {
  adminActionContainerFn,
  adminActionProjectFn,
  adminCancelBuildFn,
  adminDeleteBuildFn,
  adminDeleteImageFn,
  adminDeleteVolumeFn,
  createAdminUserFn,
  deactivateAdminUserFn,
  getAdminUsersFn,
  getAllBuildsFn,
  getAllContainersFn,
  getAllImagesFn,
  getAllProjectsFn,
  getAllVolumesFn,
  getSystemConfigFn,
  getSystemMonitoringFn,
  reactivateAdminUserFn,
  updateAdminUserFn,
  updateSystemConfigFn,
} from './api';

export function useSystemConfig() {
  return useQuery({
    queryKey: queryKeys.admin.systemConfig,
    queryFn: getSystemConfigFn,
  });
}

export function useUpdateSystemConfig() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: updateSystemConfigFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.admin.systemConfig });
      addToast(t('admin.settings.updated'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('admin.settings.updateFailed'), t).message, 'error');
    },
  });
}

export function useSystemMonitoring() {
  return useQuery({
    queryKey: queryKeys.admin.monitoring,
    queryFn: getSystemMonitoringFn,
    refetchInterval: 5000,
  });
}

export function useAdminUsers(page: number, limit: number) {
  return useQuery({
    queryKey: queryKeys.admin.users.list({ page, limit }),
    queryFn: () => getAdminUsersFn(page, limit),
  });
}

export function useCreateAdminUser() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: createAdminUserFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.admin.users.all });
      addToast(t('admin.users.created'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('admin.users.createFailed'), t).message, 'error');
    },
  });
}

export function useUpdateAdminUser() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: updateAdminUserFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.admin.users.all });
      addToast(t('admin.users.updated'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('admin.users.updateFailed'), t).message, 'error');
    },
  });
}

export function useDeactivateAdminUser() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: deactivateAdminUserFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.admin.users.all });
      addToast(t('admin.users.deactivated'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('admin.users.deactivateFailed'), t).message, 'error');
    },
  });
}

export function useReactivateAdminUser() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: reactivateAdminUserFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.admin.users.all });
      addToast(t('admin.users.activated'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('admin.users.activateFailed'), t).message, 'error');
    },
  });
}

export function useAdminContainers(page: number, limit: number, enabled: boolean) {
  return useQuery({
    queryKey: queryKeys.admin.containers.list({ page, limit }),
    queryFn: () => getAllContainersFn(page, limit),
    enabled,
    refetchInterval: (query) => {
      const containers = query.state.data?.items || [];
      return enabled && containers.some((container) => (
        ['pending', 'creating', 'starting', 'stopping', 'exposing', 'deleting'].includes(container.status) ||
        (container.status === 'running' && !!container.ttl_deadline)
      )) ? 5000 : false;
    },
  });
}

export function useAdminVolumes(page: number, limit: number, enabled: boolean) {
  return useQuery({
    queryKey: queryKeys.admin.volumes.list({ page, limit }),
    queryFn: () => getAllVolumesFn(page, limit),
    enabled,
  });
}

export function useAdminImages(page: number, limit: number, enabled: boolean) {
  return useQuery({
    queryKey: queryKeys.admin.images.list({ page, limit }),
    queryFn: () => getAllImagesFn(page, limit),
    enabled,
  });
}

export function useAdminBuilds(page: number, limit: number, enabled: boolean) {
  return useQuery({
    queryKey: queryKeys.admin.builds.list({ page, limit }),
    queryFn: () => getAllBuildsFn(page, limit),
    enabled,
    refetchInterval: (query) => {
      const builds = query.state.data?.items || [];
      return enabled && builds.some((build) => build.status === 'pending' || build.status === 'running') ? 5000 : false;
    },
  });
}

export function useAdminProjects(page: number, limit: number, enabled: boolean) {
  return useQuery({
    queryKey: queryKeys.admin.projects.list({ page, limit }),
    queryFn: () => getAllProjectsFn(page, limit),
    enabled,
  });
}

export function useAdminContainerAction() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: adminActionContainerFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.admin.containers.all });
      queryClient.invalidateQueries({ queryKey: queryKeys.containers.all });
      addToast(t('admin.resources.done'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('admin.resources.actionFailed'), t).message, 'error');
    },
  });
}

export function useAdminDeleteVolume() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: adminDeleteVolumeFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.admin.volumes.all });
      addToast(t('admin.resources.volumeDeleted'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('volumes.deleteFailed'), t).message, 'error');
    },
  });
}

export function useAdminDeleteImage() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: adminDeleteImageFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.admin.images.all });
      addToast(t('admin.resources.imageDeleted'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('images.deleteFailed'), t).message, 'error');
    },
  });
}

export function useAdminProjectAction() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: adminActionProjectFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.admin.projects.all });
      queryClient.invalidateQueries({ queryKey: queryKeys.admin.builds.all });
      addToast(t('admin.resources.done'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('admin.resources.actionFailed'), t).message, 'error');
    },
  });
}

export function useAdminDeleteBuild() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: adminDeleteBuildFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.admin.builds.all });
      addToast(t('admin.resources.buildDeleted'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('images.deleteBuildRecordFailed'), t).message, 'error');
    },
  });
}

export function useAdminCancelBuild() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: adminCancelBuildFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.admin.builds.all });
      addToast(t('images.cancelBuildRequested'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('images.cancelBuildFailed'), t).message, 'error');
    },
  });
}
