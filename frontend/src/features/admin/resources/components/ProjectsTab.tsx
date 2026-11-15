import { useState } from 'react';
import { AlertTriangle, Layers, Play, Square, Trash2, XCircle } from 'lucide-react';

import { Badge } from '@/shared/ui/Badge';
import { Button } from '@/shared/ui/Button';
import { getApiErrorMessage, getServerErrorMessage } from '@/shared/api/apiError';
import { cn } from '@/shared/lib/utils';
import { statusLabel, useLocale, useT } from '@/shared/i18n';
import { canCancelProject, isProjectBusy } from '@/features/projects/model';
import { useProjectAction, useProjects } from '@/features/projects/queries';
import { adminProjectTableLayout as layout } from './tableLayouts';
import { formatDateTime, ownerName, resourceRowClass, statusVariant } from './resourceDisplay';
import { ResourceTable } from './ResourceTable';
import { ResourceConfirmDialog, type ConfirmRequest } from './ResourceConfirmDialog';

const pageSize = 20;

export function ProjectsTab() {
  const t = useT();
  const locale = useLocale();
  const [page, setPage] = useState(1);
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null);
  // The all-users list refreshes on demand and does not poll.
  const { data, isFetching, isError, error, refetch } = useProjects('all', page, pageSize, { polling: false });
  const action = useProjectAction('all');
  const resourceError = (message?: string) => getServerErrorMessage(message, t);

  return (
    <>
      <ResourceTable
        layout={layout}
        columns={(
          <>
            <div className="pl-2">{t('projects.project')}</div>
            <div>{t('common.status')}</div>
            <div>{t('status.error')}</div>
            <div>{t('projects.createdAt')}</div>
            <div>{t('admin.resources.owner')}</div>
            <div className="flex justify-end">{t('admin.users.management')}</div>
          </>
        )}
        isFetching={isFetching}
        error={isError ? { title: t('projects.loadFailed'), message: getApiErrorMessage(error, t('projects.loadFailed'), t).message, onRetry: () => refetch() } : null}
        empty={{ icon: <Layers className="h-8 w-8" />, title: t('projects.emptyTitle'), description: t('projects.emptyDescription') }}
        itemCount={data?.items.length ?? 0}
        page={page}
        pageSize={pageSize}
        totalItems={data?.total_count}
        onPageChange={setPage}
      >
        {data?.items.map((p) => {
          const isBusy = isProjectBusy(p.status);
          const isCanceled = p.status === 'canceled';
          const projectError = resourceError(p.error_message || p.last_error);
          return (
            <div key={p.id} className={cn(resourceRowClass, layout.grid)}>
              <div className="min-w-0 pr-4 pl-2">
                <div className="font-medium block truncate" title={p.name}>{p.name}</div>
                <div className="text-xs text-slate-500 block truncate font-mono">{p.id}</div>
              </div>
              <div className="min-w-0">
                <Badge variant={statusVariant(p.status)}>
                  {statusLabel(t, p.status)}
                </Badge>
              </div>
              <div className="min-w-0 flex items-center">
                {projectError ? (
                  <div className="flex items-center gap-2 text-sm text-red-600 dark:text-red-400 truncate max-w-full cursor-help" title={projectError}>
                    <AlertTriangle className="h-3 w-3 shrink-0" />
                    <span className="truncate">{projectError}</span>
                  </div>
                ) : (
                  <span className="text-slate-400 dark:text-slate-500">-</span>
                )}
              </div>
              <div className="min-w-0 text-sm text-slate-500 dark:text-slate-400 truncate" title={formatDateTime(p.created_at, locale)}>
                {formatDateTime(p.created_at, locale)}
              </div>
              <div className="min-w-0 text-xs font-mono text-slate-500 space-y-1 pr-4">
                <div className="block truncate" title={p.owner_username || p.owner_id}>{t('admin.resources.userLabel', { value: ownerName(p) })}</div>
              </div>
              <div className="flex gap-2 justify-end shrink-0">
                <Button
                  variant="secondary"
                  className="h-8 px-2"
                  disabled={action.isPending || isBusy || isCanceled || p.status === 'failed' || p.status === 'running'}
                  onClick={() => action.mutate({ id: p.id, action: 'start' })}
                >
                  <Play className="w-4 h-4 text-green-500" />
                </Button>
                <Button
                  variant="secondary"
                  className="h-8 px-2"
                  disabled={action.isPending || isBusy || isCanceled || p.status === 'failed' || p.status === 'stopped'}
                  onClick={() => action.mutate({ id: p.id, action: 'stop' })}
                >
                  <Square className="w-4 h-4 text-yellow-500" />
                </Button>
                <Button
                  variant="secondary"
                  className="h-8 px-2"
                  disabled={action.isPending || !canCancelProject(p.status)}
                  onClick={() => setConfirm({ message: t('projects.cancelConfirm', { name: p.name }), run: () => action.mutate({ id: p.id, action: 'cancel' }), confirmLabel: t('projects.cancelRequested'), variant: 'secondary' })}
                >
                  <XCircle className="w-4 h-4 text-orange-500" />
                </Button>
                <Button
                  variant="danger"
                  className="h-8 px-2"
                  disabled={action.isPending || isBusy}
                  onClick={() => setConfirm({ message: t('projects.deleteConfirm', { name: p.name }), run: () => action.mutate({ id: p.id, action: 'delete' }) })}
                >
                  <Trash2 className="w-4 h-4" />
                </Button>
              </div>
            </div>
          );
        })}
      </ResourceTable>

      <ResourceConfirmDialog request={confirm} isLoading={action.isPending} onClose={() => setConfirm(null)} />
    </>
  );
}
