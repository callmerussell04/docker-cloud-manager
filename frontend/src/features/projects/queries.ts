import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { PageParams } from '@/shared/api/pagination';
import type { Scope } from '@/shared/api/scope';
import { queryRoots } from '@/shared/api/queryRoots';
import { useNotificationStore } from '@/shared/ui/notificationStore';
import { failureReason } from '@/shared/api/apiError';
import { useT, type TranslationKey } from '@/shared/i18n';
import { containerKeys } from '@/features/containers/queries';
import { buildKeys } from '@/features/images/queries';
import { actionProjectFn, createProjectFn, createProjectFromGitFn, getProjectsFn } from './api';
import { isProjectBusy, type CreateProjectGitPayload, type ProjectAction } from './model';

export const projectKeys = {
  all: queryRoots.projects,
  lists: (scope: Scope) => [...projectKeys.all, scope, 'list'] as const,
  list: (scope: Scope, params: PageParams) => [...projectKeys.lists(scope), params] as const,
};

// useProjects polls while a project is in a transitional state, unless
// polling is off.
export function useProjects<S extends Scope>(scope: S, page: number, limit: number, { enabled = true, polling = true }: { enabled?: boolean; polling?: boolean } = {}) {
  return useQuery({
    queryKey: projectKeys.list(scope, { page, limit }),
    queryFn: () => getProjectsFn(scope, page, limit),
    enabled,
    refetchInterval: (query) => {
      if (!enabled || !polling) return false;
      const projects = query.state.data?.items || [];
      return projects.some((project) => isProjectBusy(project.status)) ? 5000 : false;
    },
  });
}

export function useCreateProject() {
  const queryClient = useQueryClient();
  const notify = useNotificationStore((state) => state.notify);
  const t = useT();

  return useMutation({
    mutationFn: (payload: FormData | CreateProjectGitPayload) => (
      payload instanceof FormData ? createProjectFn(payload) : createProjectFromGitFn(payload)
    ),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: projectKeys.all });
      notify(t('projects.deployed'), 'success');
    },
    onError: (error: unknown) => {
      notify(t('projects.deployFailed'), 'error', failureReason(error, t));
    },
  });
}

const ownActionMessages: Record<ProjectAction, { success: TranslationKey; failure: TranslationKey }> = {
  start: { success: 'projects.startSent', failure: 'projects.startFailed' },
  stop: { success: 'projects.stopSent', failure: 'projects.stopFailed' },
  cancel: { success: 'projects.cancelRequested', failure: 'projects.cancelFailed' },
  delete: { success: 'projects.deleted', failure: 'projects.deleteFailed' },
};

export function useProjectAction(scope: Scope) {
  const queryClient = useQueryClient();
  const notify = useNotificationStore((state) => state.notify);
  const t = useT();

  return useMutation({
    mutationFn: (variables: { id: string; action: ProjectAction }) => actionProjectFn({ scope, ...variables }),
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: projectKeys.all });
      queryClient.invalidateQueries({ queryKey: containerKeys.all });
      queryClient.invalidateQueries({ queryKey: buildKeys.all });
      notify(t(scope === 'all' ? 'admin.resources.done' : ownActionMessages[variables.action].success), 'success');
    },
    onError: (error: unknown, variables) => {
      const fallback = scope === 'all' ? 'admin.resources.actionFailed' : ownActionMessages[variables.action].failure;
      notify(t(fallback), 'error', failureReason(error, t));
    },
  });
}
