import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Activity, AlertTriangle, ExternalLink, Globe, Play, ScrollText, Square, Terminal, Trash2 } from 'lucide-react';

import { Badge } from '@/shared/ui/Badge';
import { Button } from '@/shared/ui/Button';
import { getApiErrorMessage, getServerErrorMessage } from '@/shared/api/apiError';
import { BASE_DOMAIN } from '@/shared/lib/config';
import { cn } from '@/shared/lib/utils';
import { useT } from '@/shared/i18n';
import { ContainerTTLTimer } from '@/features/containers/components/ContainerTTLTimer';
import { blockedContainerActions, containerPagePath, containerBadgeVariant, containerStatusLabel, type AdminContainerData, type ConsoleView } from '@/features/containers/model';
import { useContainerAction, useContainers } from '@/features/containers/queries';
import { ownerName, resourceRowClass } from './resourceDisplay';
import { ResourceTable } from './ResourceTable';
import { ResourceConfirmDialog, type ConfirmRequest } from './ResourceConfirmDialog';

// Grid classes shared by this table's header and rows.
const layout = {
  minWidth: 'min-w-[1340px]',
  grid: 'grid-cols-[minmax(260px,1.7fr)_minmax(150px,0.8fr)_minmax(240px,1.3fr)_minmax(260px,1.5fr)_19rem]',
} as const;

const pageSize = 20;

export function ContainersTab() {
  const t = useT();
  const [page, setPage] = useState(1);
  const navigate = useNavigate();
  // Logs and the terminal open on the container's page.
  const openConsole = (container: AdminContainerData, view: ConsoleView) =>
    navigate(containerPagePath('/admin/containers', container.id, view), { state: { container } });
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null);
  const { data, isLoading, isFetching, isError, error, refetch } = useContainers('all', page, pageSize);
  const action = useContainerAction('all');
  const resourceError = (message?: string) => getServerErrorMessage(message, t);

  return (
    <>
      <ResourceTable
        layout={layout}
        columns={(
          <>
            <div className="pl-2">{t('containers.name')} / ID</div>
            <div>{t('common.status')}</div>
            <div>{t('containers.routing')}</div>
            <div>{t('admin.resources.owner')} / Docker ID</div>
            <div className="flex justify-end">{t('admin.users.management')}</div>
          </>
        )}
        isLoading={isLoading}
        isFetching={isFetching}
        error={isError ? { title: t('containers.loadFailed'), message: getApiErrorMessage(error, t('containers.loadFailed'), t).message, onRetry: () => refetch() } : null}
        empty={{ title: t('containers.emptyTitle'), description: t('containers.emptyDescription') }}
        itemCount={data?.items.length ?? 0}
        page={page}
        pageSize={pageSize}
        totalItems={data?.total_count}
        onPageChange={setPage}
      >
        {data?.items.map((c) => {
          const blocked = blockedContainerActions(c.status);
          return (
            <div key={c.id} className={cn(resourceRowClass, layout.grid)}>
              <div className="min-w-0 pr-4 pl-2">
                <Link to={`/admin/containers/${c.id}`} state={{ container: c }} className="font-medium block truncate hover:underline text-slate-900 dark:text-slate-100">{c.name}</Link>
                <div className="text-xs text-slate-500 block truncate font-mono">{c.id}</div>
              </div>
              <div className="min-w-0">
                <Badge variant={containerBadgeVariant(c.status)}>{containerStatusLabel(t, c.status)}</Badge>
                <ContainerTTLTimer status={c.status} ttlDeadline={c.ttl_deadline} />
                {resourceError(c.last_error) && (
                  <div className="mt-1 flex items-center gap-1 text-xs text-red-600 dark:text-red-400" title={resourceError(c.last_error)}>
                    <AlertTriangle className="h-3 w-3 shrink-0" />
                    <span className="truncate">{resourceError(c.last_error)}</span>
                  </div>
                )}
              </div>
              <div className="min-w-0 flex items-center text-sm text-slate-600 dark:text-slate-300 pr-4">
                {c.domain_prefix ? (
                  <a
                    href={`http://${c.domain_prefix}.${BASE_DOMAIN}`}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex min-w-0 items-center gap-2 text-accent transition-colors hover:underline"
                    title={`${c.domain_prefix}.${BASE_DOMAIN}:${c.internal_port}`}
                  >
                    <Globe className="h-4 w-4 shrink-0" />
                    <span className="truncate">{c.domain_prefix}.{BASE_DOMAIN}</span>
                    <ExternalLink className="h-3 w-3 shrink-0 opacity-70" />
                  </a>
                ) : (
                  <div className="flex min-w-0 items-center gap-2 text-slate-400" title={t('common.notRouted')}>
                    <Globe className="h-4 w-4 shrink-0" />
                    <span className="truncate">{t('common.notRouted')}</span>
                  </div>
                )}
              </div>
              <div className="min-w-0 text-xs font-mono text-slate-500 space-y-1">
                <div className="block truncate" title={c.owner_username || c.owner_id}>{t('admin.resources.userLabel', { value: ownerName(c) })}</div>
                <div className="block truncate text-slate-400" title={c.docker_id}>{t('admin.resources.dockerIdLabel', { value: c.docker_id?.slice(0, 12) || 'N/A' })}</div>
              </div>
              <div className="flex gap-2 justify-end shrink-0">
                <Link title={t('containers.stats')} aria-label={t('containers.stats')} to={`/admin/containers/${c.id}`} state={{ container: c }} className="p-2 rounded-lg bg-fill text-accent hover:bg-fill-hover transition-colors">
                  <Activity className="w-4 h-4" />
                </Link>

                <button
                  onClick={() => openConsole(c, 'terminal')}
                  disabled={blocked.terminal}
                  className="p-2 rounded-lg bg-slate-100 text-slate-700 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-400 dark:hover:bg-slate-700 transition-colors disabled:opacity-50 disabled:pointer-events-none"
                  title={t('containers.terminal')} aria-label={t('containers.terminal')}
                >
                  <Terminal className="w-4 h-4" />
                </button>

                <button title={t('containers.logs')} aria-label={t('containers.logs')}
                  onClick={() => openConsole(c, 'logs')}
                  className="p-2 rounded-lg bg-slate-100 text-slate-700 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-400 dark:hover:bg-slate-700 transition-colors"
                >
                  <ScrollText className="w-4 h-4" />
                </button>

                <div className="w-px h-6 bg-slate-200 dark:bg-slate-700 mx-1" />

                <Button title={t('containers.start')} aria-label={t('containers.start')} variant="secondary" className="h-8 px-2" disabled={action.isPending || blocked.start} onClick={() => action.mutate({ id: c.id, action: 'start' })}><Play className="w-4 h-4 text-green-500" /></Button>
                <Button title={t('containers.stop')} aria-label={t('containers.stop')} variant="secondary" className="h-8 px-2" disabled={action.isPending || blocked.stop} onClick={() => action.mutate({ id: c.id, action: 'stop' })}><Square className="w-4 h-4 text-yellow-500" /></Button>
                <Button title={t('common.delete')} aria-label={t('common.delete')}
                  variant="danger"
                  className="h-8 px-2"
                  disabled={action.isPending || blocked.delete}
                  onClick={() => setConfirm({ title: t('containers.deleteConfirm', { name: c.name }), message: t('containers.deleteConsequence'), run: () => action.mutate({ id: c.id, action: 'delete' }) })}
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
