import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { queryKeys } from '@/shared/api/queryKeys';
import { useToastStore } from '@/store/toastStore';
import { getApiErrorMessage } from '@/lib/apiError';
import { useT } from '@/lib/i18n';
import {
  cancelProjectFn,
  createProjectFn,
  createProjectFromGitFn,
  deleteProjectFn,
  getProjectsFn,
  startProjectFn,
  stopProjectFn,
} from './api';
import type { CreateProjectGitPayload } from './types';

const activeProjectStatuses = new Set(['pending', 'building', 'deploying', 'canceling', 'starting', 'stopping', 'deleting']);

export function useProjects(page: number, limit: number) {
  return useQuery({
    queryKey: queryKeys.projects.list({ page, limit }),
    queryFn: () => getProjectsFn(page, limit),
    refetchInterval: (query) => {
      const projects = query.state.data?.items || [];
      return projects.some((project) => activeProjectStatuses.has(project.status)) ? 5000 : false;
    },
  });
}

export function useCreateProject() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: (payload: FormData | CreateProjectGitPayload) => (
      payload instanceof FormData ? createProjectFn(payload) : createProjectFromGitFn(payload)
    ),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.projects.all });
      addToast(t('projects.deployed'), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t('projects.deployFailed'), t).message, 'error');
    },
  });
}

function useProjectActionMutation(actionFn: (id: string) => Promise<void>, successKey: 'projects.deleted' | 'projects.startSent' | 'projects.stopSent' | 'projects.cancelRequested', errorKey: 'projects.deleteFailed' | 'projects.startFailed' | 'projects.stopFailed' | 'projects.cancelFailed') {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  return useMutation({
    mutationFn: actionFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.projects.all });
      queryClient.invalidateQueries({ queryKey: queryKeys.builds.all });
      addToast(t(successKey), 'success');
    },
    onError: (error: unknown) => {
      addToast(getApiErrorMessage(error, t(errorKey), t).message, 'error');
    },
  });
}

export function useDeleteProject() {
  return useProjectActionMutation(deleteProjectFn, 'projects.deleted', 'projects.deleteFailed');
}

export function useStartProject() {
  return useProjectActionMutation(startProjectFn, 'projects.startSent', 'projects.startFailed');
}

export function useStopProject() {
  return useProjectActionMutation(stopProjectFn, 'projects.stopSent', 'projects.stopFailed');
}

export function useCancelProject() {
  return useProjectActionMutation(cancelProjectFn, 'projects.cancelRequested', 'projects.cancelFailed');
}
