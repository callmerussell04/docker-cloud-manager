import { useLocation, useNavigate, useParams, Link } from 'react-router-dom';
import type { Scope } from '@/shared/api/scope';
import { Activity, ArrowLeft, Cpu, HardDrive, Network, Box, Terminal, ScrollText, Play, Square, Globe, Trash2 } from 'lucide-react';
import { useState } from 'react';

import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/Card';
import { Badge } from '@/shared/ui/Badge';
import { Button } from '@/shared/ui/Button';
import { formatBytes } from '@/shared/lib/utils';
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';
import { ErrorState } from '@/shared/ui/ErrorState';
import { getApiErrorMessage, isNotFoundError } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import {
  blockedContainerActions,
  borrowedMemoryBytes,
  containerBadgeVariant,
  containerStatusLabel,
  type ContainerAction,
  type ContainerData,
} from '../model';
import { ContainerTTLTimer } from '../components/ContainerTTLTimer';
import { useContainerModals } from '../components/useContainerModals';
import { useContainer, useContainerAction, useContainerStats } from '../queries';

interface ContainerDetailsPageProps {
  scope: Scope;
  // backTo is the list the back link returns to. The route supplies it, so
  // this page does not know the admin area's URLs.
  backTo: string;
}

export function ContainerDetailsPage({ scope, backTo }: ContainerDetailsPageProps) {
  const t = useT();
  const { id } = useParams<{ id: string }>();
  const location = useLocation();
  const navigate = useNavigate();

  // The list passes the row it was opened from, which is shown until the
  // container's current state arrives.
  const snapshot = location.state?.container as ContainerData | undefined;
  const containerQuery = useContainer(scope, id, snapshot);
  // A container that is gone is not shown from the last answer that had it.
  const container = isNotFoundError(containerQuery.error) ? undefined : containerQuery.data;
  const running = container?.status === 'running';

  const { openLogs, openTerminal, openExpose, modals } = useContainerModals(scope);
  const [isDeleteConfirmOpen, setIsDeleteConfirmOpen] = useState(false);

  const statsQuery = useContainerStats(scope, id, running);
  // Stats kept from an earlier run are not those of a stopped container.
  const stats = running ? statsQuery.data : undefined;
  const isLoading = container ? running && statsQuery.isLoading : containerQuery.isLoading;
  const actionMutation = useContainerAction(scope);

  const memPercent = stats?.memory_limit_bytes 
    ? (stats.memory_usage_bytes / stats.memory_limit_bytes) * 100 
    : 0;
  const standardLimit = stats?.memory_standard_limit_bytes || 0;
  const borrowedMemory = borrowedMemoryBytes(stats);
  const blocked = blockedContainerActions(container?.status ?? '');
  const isUserContainer = !!container && scope === 'own';

  const handleContainerAction = (action: ContainerAction) => {
    if (!container) return;
    if (action === 'delete') {
      setIsDeleteConfirmOpen(true);
      return;
    }
    actionMutation.mutate({ id: container.id, action });
  };

  if (!container && containerQuery.isError) {
    return (
      <div className="space-y-6">
        <div className="flex items-center gap-4">
          <Link
            to={backTo}
            className="p-2 rounded-xl bg-white/40 dark:bg-slate-800/40 hover:bg-white/60 dark:hover:bg-slate-700/50 border border-white/50 dark:border-slate-700/50 transition-colors"
          >
            <ArrowLeft className="w-5 h-5" />
          </Link>
          <h1 className="text-3xl font-bold tracking-tight truncate">{snapshot?.name || id}</h1>
        </div>
        <ErrorState
          title={t('containers.detailsLoadFailed')}
          message={getApiErrorMessage(containerQuery.error, t('containers.detailsLoadFailed'), t).message}
          onRetry={isNotFoundError(containerQuery.error) ? undefined : () => containerQuery.refetch()}
          isRetrying={containerQuery.isFetching}
        />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-4">
          <Link 
            to={backTo} 
            className="p-2 rounded-xl bg-white/40 dark:bg-slate-800/40 hover:bg-white/60 dark:hover:bg-slate-700/50 border border-white/50 dark:border-slate-700/50 transition-colors"
          >
            <ArrowLeft className="w-5 h-5" />
          </Link>
          <div>
            <div className="flex items-center gap-3">
              <h1 className="text-3xl font-bold tracking-tight">{container?.name || id}</h1>
              {container && <Badge variant={containerBadgeVariant(container.status)}>{containerStatusLabel(t, container.status)}</Badge>}
            </div>
            {container && (
              <ContainerTTLTimer status={container.status} ttlDeadline={container.ttl_deadline} className="mt-2" />
            )}
            {container && (
              <div className="flex items-center gap-2 mt-1 text-sm text-slate-500 dark:text-slate-400">
                <Box className="w-4 h-4" />
                <span>{container.image_tag}</span>
              </div>
            )}
          </div>
        </div>

        {container && (
          <div className="flex flex-wrap items-center justify-end gap-2">
            <button
              onClick={() => openTerminal(container)}
              disabled={blocked.terminal}
              className="flex items-center gap-2 px-4 py-2.5 rounded-xl bg-slate-100 text-slate-700 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:hover:bg-slate-700 transition-colors disabled:opacity-50 disabled:pointer-events-none"
              title={t('containers.terminal')}
            >
              <Terminal className="w-4 h-4" />
              <span className="hidden sm:inline font-medium text-sm">{t('containers.terminal')}</span>
            </button>
            <button
              onClick={() => openLogs(container)}
              className="flex items-center gap-2 px-4 py-2.5 rounded-xl bg-slate-100 text-slate-700 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:hover:bg-slate-700 transition-colors"
              title={t('containers.logs')}
            >
              <ScrollText className="w-4 h-4" />
              <span className="hidden sm:inline font-medium text-sm">{t('containers.logs')}</span>
            </button>
            {isUserContainer && (
              <>
                <div className="h-8 w-px bg-slate-200 dark:bg-slate-700 mx-1" />
                <Button
                  variant="secondary"
                  className="px-3"
                  disabled={blocked.start || actionMutation.isPending}
                  onClick={() => handleContainerAction('start')}
                  title={t('containers.start')}
                >
                  <Play className="w-4 h-4 text-green-500" />
                  <span className="hidden lg:inline ml-2">{t('containers.start')}</span>
                </Button>
                <Button
                  variant="secondary"
                  className="px-3"
                  disabled={blocked.stop || actionMutation.isPending}
                  onClick={() => handleContainerAction('stop')}
                  title={t('containers.stop')}
                >
                  <Square className="w-4 h-4 text-yellow-500" />
                  <span className="hidden lg:inline ml-2">{t('containers.stop')}</span>
                </Button>
                <Button
                  variant="secondary"
                  className="px-3"
                  disabled={blocked.expose || actionMutation.isPending}
                  onClick={() => openExpose(container)}
                  title={t('containers.routingSettings')}
                >
                  <Globe className="w-4 h-4 text-blue-500" />
                  <span className="hidden lg:inline ml-2">{t('containers.routingSettings')}</span>
                </Button>
                <Button
                  variant="danger"
                  className="px-3"
                  disabled={blocked.delete || actionMutation.isPending}
                  onClick={() => handleContainerAction('delete')}
                  title={t('common.delete')}
                >
                  <Trash2 className="w-4 h-4" />
                </Button>
              </>
            )}
          </div>
        )}
      </div>

      {running && statsQuery.isError && (
        <ErrorState
          message={getApiErrorMessage(statsQuery.error, t('containers.statsLoadFailed'), t).message}
          onRetry={() => statsQuery.refetch()}
          isRetrying={statsQuery.isFetching}
        />
      )}

      {container && !running && (
        <div className="bg-yellow-50 dark:bg-yellow-900/20 text-yellow-800 dark:text-yellow-300 p-4 rounded-xl border border-yellow-200 dark:border-yellow-900/50 text-sm flex items-center gap-2">
          <Activity className="w-4 h-4" />
          {t('containers.statsOnlyRunning')}
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        <Card>
          <CardHeader className="pb-4">
            <CardTitle className="text-lg flex items-center gap-2">
              <Cpu className="w-5 h-5 text-indigo-500" />
              {t('containers.cpu')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <div className="h-16 animate-pulse bg-slate-200 dark:bg-slate-800 rounded-lg" />
            ) : (
              <div className="space-y-4">
                <div className="flex justify-between items-end">
                  <span className="text-3xl font-semibold">{stats?.cpu_percentage?.toFixed(2) || '0.00'}%</span>
                </div>
                <div className="w-full bg-slate-100 dark:bg-slate-800 rounded-full h-3 border border-slate-200 dark:border-slate-700 overflow-hidden">
                  <div 
                    className="bg-indigo-500 h-full rounded-full transition-all duration-500" 
                    style={{ width: `${Math.min(stats?.cpu_percentage || 0, 100)}%` }} 
                  />
                </div>
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-4">
            <CardTitle className="text-lg flex items-center gap-2">
              <HardDrive className="w-5 h-5 text-emerald-500" />
              {t('containers.memory')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <div className="h-16 animate-pulse bg-slate-200 dark:bg-slate-800 rounded-lg" />
            ) : (
              <div className="space-y-4">
                <div className="flex justify-between items-end">
                  <span className="text-3xl font-semibold">{formatBytes(stats?.memory_usage_bytes || 0)}</span>
                  <span className="text-sm text-slate-500 dark:text-slate-400 mb-1">/ {formatBytes(stats?.memory_limit_bytes || 0)}</span>
                </div>
                <div className="w-full bg-slate-100 dark:bg-slate-800 rounded-full h-3 border border-slate-200 dark:border-slate-700 overflow-hidden">
                  <div 
                    className="bg-emerald-500 h-full rounded-full transition-all duration-500" 
                    style={{ width: `${Math.min(memPercent, 100)}%` }} 
                  />
                </div>
                {standardLimit > 0 && (
                  <div className="flex flex-wrap items-center justify-between gap-2 text-sm text-slate-500 dark:text-slate-400">
                    <span>{t('containers.standardLimit')}: {formatBytes(standardLimit)}</span>
                    {borrowedMemory > 0 && (
                      <Badge variant="warning">{t('containers.borrowedMemory')}: {formatBytes(borrowedMemory)}</Badge>
                    )}
                  </div>
                )}
                {borrowedMemory > 0 && (
                  <p className="text-xs text-slate-500 dark:text-slate-400">{t('containers.borrowedMemoryNote')}</p>
                )}
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-4">
            <CardTitle className="text-lg flex items-center gap-2">
              <Network className="w-5 h-5 text-blue-500" />
              {t('containers.network')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <div className="h-16 animate-pulse bg-slate-200 dark:bg-slate-800 rounded-lg" />
            ) : (
              <div className="space-y-4">
                <div className="flex items-center justify-between p-3 bg-slate-50 dark:bg-slate-800/50 rounded-xl border border-slate-100 dark:border-slate-700/50">
                  <span className="text-sm text-slate-500 dark:text-slate-400">{t('containers.rx')}</span>
                  <span className="font-semibold">{formatBytes(stats?.network_rx_bytes || 0)}</span>
                </div>
                <div className="flex items-center justify-between p-3 bg-slate-50 dark:bg-slate-800/50 rounded-xl border border-slate-100 dark:border-slate-700/50">
                  <span className="text-sm text-slate-500 dark:text-slate-400">{t('containers.tx')}</span>
                  <span className="font-semibold">{formatBytes(stats?.network_tx_bytes || 0)}</span>
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {modals}

      <ConfirmDialog
        isOpen={isDeleteConfirmOpen}
        title={t('confirm.title')}
        message={t('containers.deleteConfirm', { name: container?.name || id || '' })}
        confirmLabel={t('common.delete')}
        isLoading={actionMutation.isPending}
        onCancel={() => setIsDeleteConfirmOpen(false)}
        onConfirm={() => {
          if (!container) return;
          actionMutation.mutate(
            { id: container.id, action: 'delete' },
            { onSuccess: () => navigate(backTo) },
          );
        }}
      />
    </div>
  );
}
