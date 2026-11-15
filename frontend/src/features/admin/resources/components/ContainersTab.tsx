import { useState } from 'react';
import { Link } from 'react-router-dom';
import { Activity, AlertTriangle, Box, ExternalLink, Globe, Play, ScrollText, Square, Terminal, Trash2 } from 'lucide-react';

import { Badge } from '@/shared/ui/Badge';
import { Button } from '@/shared/ui/Button';
import { getApiErrorMessage, getServerErrorMessage } from '@/shared/api/apiError';
import { BASE_DOMAIN } from '@/shared/lib/config';
import { cn } from '@/shared/lib/utils';
import { statusLabel, useT } from '@/shared/i18n';
import { ContainerLogsModal } from '@/features/containers/components/ContainerLogsModal';
import { ContainerTerminalModal } from '@/features/containers/components/ContainerTerminalModal';
import { ContainerTTLTimer } from '@/features/containers/components/ContainerTTLTimer';
import type { AdminContainerData } from '@/features/containers/model';
import { useContainerAction, useContainers } from '@/features/containers/queries';
import { adminContainerTableLayout as layout } from './tableLayouts';
import { ownerName, resourceRowClass, statusVariant } from './resourceDisplay';
import { ResourceTable } from './ResourceTable';
import { ResourceConfirmDialog, type ConfirmRequest } from './ResourceConfirmDialog';

const pageSize = 20;

function isContainerActionBlocked(status: string) {
  return ['creating', 'starting', 'stopping', 'exposing', 'deleting', 'missing', 'reconciling'].includes(status);
}

export function ContainersTab() {
  const t = useT();
  const [page, setPage] = useState(1);
  const [logsContainer, setLogsContainer] = useState<AdminContainerData | null>(null);
  const [terminalContainer, setTerminalContainer] = useState<AdminContainerData | null>(null);
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null);
  const { data, isFetching, isError, error, refetch } = useContainers('all', page, pageSize);
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
        isFetching={isFetching}
        error={isError ? { title: t('containers.loadFailed'), message: getApiErrorMessage(error, t('containers.loadFailed'), t).message, onRetry: () => refetch() } : null}
        empty={{ icon: <Box className="h-8 w-8" />, title: t('containers.emptyTitle'), description: t('containers.emptyDescription') }}
        itemCount={data?.items.length ?? 0}
        page={page}
        pageSize={pageSize}
        totalItems={data?.total_count}
        onPageChange={setPage}
      >
        {data?.items.map((c) => (
          <div key={c.id} className={cn(resourceRowClass, layout.grid)}>
            <div className="min-w-0 pr-4 pl-2">
              <Link to={`/admin/containers/${c.id}`} state={{ container: c }} className="font-medium block truncate hover:underline text-slate-900 dark:text-slate-100">{c.name}</Link>
              <div className="text-xs text-slate-500 block truncate font-mono">{c.id}</div>
            </div>
            <div className="min-w-0">
              <Badge variant={statusVariant(c.status)}>{statusLabel(t, c.status)}</Badge>
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
                  className="inline-flex min-w-0 items-center gap-2 text-indigo-600 transition-colors hover:text-indigo-700 dark:text-indigo-400 dark:hover:text-indigo-300"
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
              <Link to={`/admin/containers/${c.id}`} state={{ container: c }} className="p-2 rounded-lg bg-indigo-100 text-indigo-700 hover:bg-indigo-200 dark:bg-indigo-900/30 dark:text-indigo-400 dark:hover:bg-indigo-900/50 transition-colors">
                <Activity className="w-4 h-4" />
              </Link>

              <button
                onClick={() => setTerminalContainer(c)}
                disabled={c.status !== 'running'}
                className="p-2 rounded-lg bg-slate-100 text-slate-700 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-400 dark:hover:bg-slate-700 transition-colors disabled:opacity-50 disabled:pointer-events-none"
                title={t('containers.terminal')}
              >
                <Terminal className="w-4 h-4" />
              </button>

              <button
                onClick={() => setLogsContainer(c)}
                className="p-2 rounded-lg bg-slate-100 text-slate-700 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-400 dark:hover:bg-slate-700 transition-colors"
              >
                <ScrollText className="w-4 h-4" />
              </button>

              <div className="w-px h-6 bg-slate-200 dark:bg-slate-700 mx-1" />

              <Button variant="secondary" className="h-8 px-2" disabled={action.isPending || c.status === 'running' || isContainerActionBlocked(c.status)} onClick={() => action.mutate({ id: c.id, action: 'start' })}><Play className="w-4 h-4 text-green-500" /></Button>
              <Button variant="secondary" className="h-8 px-2" disabled={action.isPending || c.status !== 'running'} onClick={() => action.mutate({ id: c.id, action: 'stop' })}><Square className="w-4 h-4 text-yellow-500" /></Button>
              <Button
                variant="danger"
                className="h-8 px-2"
                disabled={action.isPending || ['exposing', 'deleting'].includes(c.status)}
                onClick={() => setConfirm({ message: t('containers.deleteConfirm', { name: c.name }), run: () => action.mutate({ id: c.id, action: 'delete' }) })}
              >
                <Trash2 className="w-4 h-4" />
              </Button>
            </div>
          </div>
        ))}
      </ResourceTable>

      <ContainerLogsModal container={logsContainer} scope="all" onClose={() => setLogsContainer(null)} />
      <ContainerTerminalModal container={terminalContainer} scope="all" onClose={() => setTerminalContainer(null)} />
      <ResourceConfirmDialog request={confirm} isLoading={action.isPending} onClose={() => setConfirm(null)} />
    </>
  );
}
