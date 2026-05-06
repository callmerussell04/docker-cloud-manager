import { Play, Square, Trash2, Globe, ExternalLink, Box, Activity, Terminal, ScrollText, AlertTriangle } from 'lucide-react';
import { Badge } from '@/components/ui/Badge';
import { type ContainerData } from '../types';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { actionContainerFn } from '../api';
import { useToastStore } from '@/store/toastStore';
import { BASE_DOMAIN } from '@/config';
import { Link } from 'react-router-dom';
import { getApiErrorMessage } from '@/lib/apiError';
import { tableLayouts } from '@/components/ui/tableLayouts';
import { cn } from '@/lib/utils';
import { statusLabel, useT, type TranslationKey } from '@/lib/i18n';

interface ContainerRowProps {
  container: ContainerData;
  onExpose: (container: ContainerData) => void;
  onViewLogs: (container: ContainerData) => void;
  onOpenTerminal: (container: ContainerData) => void;
}

export function ContainerRow({ container, onExpose, onViewLogs, onOpenTerminal }: ContainerRowProps) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  const actionMutation = useMutation({
    mutationFn: actionContainerFn,
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: ['containers'] });
      queryClient.invalidateQueries({ queryKey: ['admin_containers'] });
      addToast(t('containers.actionSent', { action: t(`containers.action.${variables.action}` as TranslationKey) }), 'success');
    },
    onError: (error: unknown) => {
      const { message, requestId } = getApiErrorMessage(error, t('containers.actionFailed'), t);
      addToast(message, 'error', { requestId });
    },
  });

  const getStatusBadge = (status: string) => {
    switch (status) {
      case 'running': return <Badge variant="success">{statusLabel(t, status)}</Badge>;
      case 'exited': return <Badge variant="default">{statusLabel(t, status)}</Badge>;
      case 'creating': return <Badge variant="warning">{statusLabel(t, status)}</Badge>;
      case 'starting': return <Badge variant="info">{statusLabel(t, status)}</Badge>;
      case 'stopping': return <Badge variant="warning">{statusLabel(t, status)}</Badge>;
      case 'created': return <Badge variant="default">{statusLabel(t, status)}</Badge>;
      case 'deleting': return <Badge variant="warning">{statusLabel(t, status)}</Badge>;
      case 'missing': return <Badge variant="error">{statusLabel(t, status)}</Badge>;
      case 'reconciling': return <Badge variant="warning">{statusLabel(t, status)}</Badge>;
      case 'error': return <Badge variant="error">{statusLabel(t, status)}</Badge>;
      default: return <Badge variant="info">{status}</Badge>;
    }
  };

  const handleAction = (action: 'start' | 'stop' | 'delete') => {
    if (action === 'delete' && !window.confirm(t('containers.deleteConfirm', { name: container.name }))) {
      return;
    }
    actionMutation.mutate({ id: container.id, action });
  };

  return (
    <div className={cn("grid gap-4 p-4 items-center hover:bg-white/20 dark:hover:bg-slate-800/30 transition-colors border-b border-white/20 dark:border-slate-700/50 last:border-0", tableLayouts.containers.grid, tableLayouts.containers.minWidth)}>
      <div className="flex items-center gap-4 min-w-0 pr-4">
        <div className="w-10 h-10 rounded-xl bg-indigo-100 dark:bg-indigo-900/30 text-indigo-600 dark:text-indigo-400 flex items-center justify-center shrink-0">
          <Box className="w-5 h-5" />
        </div>
        <div className="min-w-0 flex-1">
          <Link to={`/containers/${container.id}`} state={{ container }} className="font-semibold text-base block truncate hover:underline text-slate-900 dark:text-slate-100" title={container.name}>
            {container.name}
          </Link>
          <p className="text-xs text-slate-500 dark:text-slate-400 block truncate" title={container.image_tag}>
            {container.image_tag}
          </p>
        </div>
      </div>

      <div className="min-w-0 shrink-0">
        {getStatusBadge(container.status)}
        {container.last_error && (
          <div className="flex items-center gap-1 text-xs text-red-600 dark:text-red-400 truncate mt-1" title={container.last_error}>
            <AlertTriangle className="w-3 h-3 shrink-0" />
            <span className="truncate">{container.last_error}</span>
          </div>
        )}
      </div>

      <div className="min-w-0 flex items-center text-sm text-slate-600 dark:text-slate-300 pr-4">
        {container.domain_prefix ? (
          <a 
            href={`http://${container.domain_prefix}.${BASE_DOMAIN}`}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-2 text-indigo-600 dark:text-indigo-400 hover:text-indigo-700 dark:hover:text-indigo-300 transition-colors min-w-0 w-full"
            title={`${container.domain_prefix}.${BASE_DOMAIN}:${container.internal_port}`}
          >
            <Globe className="w-4 h-4 shrink-0" />
            <span className="truncate">{container.domain_prefix}.{BASE_DOMAIN}</span>
            <ExternalLink className="w-3 h-3 shrink-0 opacity-70" />
          </a>
        ) : (
          <div className="flex items-center gap-2 text-slate-400 min-w-0" title={t('common.notRouted')}>
            <Globe className="w-4 h-4 shrink-0" />
            <span className="truncate">{t('common.notRouted')}</span>
          </div>
        )}
      </div>

      <div className="flex items-center gap-2 justify-end shrink-0">
        <Link
          to={`/containers/${container.id}`}
          state={{ container }}
          className="p-2 rounded-lg bg-indigo-100 text-indigo-700 hover:bg-indigo-200 dark:bg-indigo-900/30 dark:text-indigo-400 dark:hover:bg-indigo-900/50 transition-colors"
          title={t('containers.stats')}
        >
          <Activity className="w-4 h-4" />
        </Link>

        <button
          onClick={() => onOpenTerminal(container)}
          disabled={container.status !== 'running'}
          className="p-2 rounded-lg bg-slate-100 text-slate-700 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-400 dark:hover:bg-slate-700 transition-colors"
          title={t('containers.terminal')}
        >
          <Terminal className="w-4 h-4" />
        </button>

        <button
          onClick={() => onViewLogs(container)}
          className="p-2 rounded-lg bg-slate-100 text-slate-700 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-400 dark:hover:bg-slate-700 transition-colors"
          title={t('containers.logs')}
        >
          <ScrollText className="w-4 h-4" />
        </button>

        <div className="w-px h-6 bg-slate-200 dark:bg-slate-700 mx-1" />

        <button
          onClick={() => handleAction('start')}
          disabled={['running', 'creating', 'starting', 'stopping', 'deleting', 'missing', 'reconciling'].includes(container.status) || actionMutation.isPending}
          className="p-2 rounded-lg bg-green-100 text-green-700 hover:bg-green-200 dark:bg-green-900/30 dark:text-green-400 dark:hover:bg-green-900/50 disabled:opacity-50 transition-colors"
          title={t('containers.start')}
        >
          <Play className="w-4 h-4" />
        </button>
        
        <button
          onClick={() => handleAction('stop')}
          disabled={container.status !== 'running' || actionMutation.isPending}
          className="p-2 rounded-lg bg-yellow-100 text-yellow-700 hover:bg-yellow-200 dark:bg-yellow-900/30 dark:text-yellow-400 dark:hover:bg-yellow-900/50 disabled:opacity-50 transition-colors"
          title={t('containers.stop')}
        >
          <Square className="w-4 h-4" />
        </button>

        <button
          onClick={() => onExpose(container)}
          disabled={['creating', 'starting', 'stopping', 'deleting', 'missing', 'reconciling'].includes(container.status) || actionMutation.isPending}
          className="p-2 rounded-lg bg-blue-100 text-blue-700 hover:bg-blue-200 dark:bg-blue-900/30 dark:text-blue-400 dark:hover:bg-blue-900/50 disabled:opacity-50 transition-colors"
          title={t('containers.routingSettings')}
        >
          <Globe className="w-4 h-4" />
        </button>

        <button
          onClick={() => handleAction('delete')}
          disabled={container.status === 'deleting' || actionMutation.isPending}
          className="p-2 rounded-lg bg-red-100 text-red-700 hover:bg-red-200 dark:bg-red-900/30 dark:text-red-400 dark:hover:bg-red-900/50 disabled:opacity-50 transition-colors ml-2"
          title={t('common.delete')}
        >
          <Trash2 className="w-4 h-4" />
        </button>
      </div>
    </div>
  );
}
