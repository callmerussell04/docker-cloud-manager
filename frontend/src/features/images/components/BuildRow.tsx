import { Terminal, Trash2, Clock, CheckCircle2, AlertCircle, Loader2, Info, XCircle } from 'lucide-react';
import { Badge } from '@/components/ui/Badge';
import { type BuildData } from '../types';
import { tableLayouts } from '@/components/ui/tableLayouts';
import { cn } from '@/lib/utils';
import { dateLocale, statusLabel, useLocale, useT } from '@/lib/i18n';
import { useCancelBuild, useDeleteBuild } from '../hooks';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { useState } from 'react';

interface BuildRowProps {
  build: BuildData;
  onViewLogs: (build: BuildData) => void;
}

const terminalBuildStatuses = new Set(['success', 'failed', 'failed_timeout', 'failed_quota_exceeded', 'failed_resource_exhausted', 'failed_internal', 'canceled']);

export function BuildRow({ build, onViewLogs }: BuildRowProps) {
  const t = useT();
  const locale = useLocale();
  const [confirmAction, setConfirmAction] = useState<'cancel' | 'delete' | null>(null);
  const deleteMutation = useDeleteBuild();
  const cancelMutation = useCancelBuild();

  const getStatusDisplay = (status: string) => {
    switch (status) {
      case 'success': 
        return { icon: <CheckCircle2 className="w-4 h-4" />, variant: 'success' as const, label: statusLabel(t, status) };
      case 'running': 
        return { icon: <Loader2 className="w-4 h-4 animate-spin" />, variant: 'info' as const, label: t('status.runningBuild') };
      case 'pending': 
        return { icon: <Clock className="w-4 h-4" />, variant: 'warning' as const, label: statusLabel(t, status) };
      case 'failed': 
        return { icon: <AlertCircle className="w-4 h-4" />, variant: 'error' as const, label: statusLabel(t, status) };
      case 'failed_timeout': 
        return { icon: <AlertCircle className="w-4 h-4" />, variant: 'error' as const, label: statusLabel(t, status) };
      case 'failed_quota_exceeded': 
        return { icon: <AlertCircle className="w-4 h-4" />, variant: 'error' as const, label: statusLabel(t, status) };
      case 'failed_resource_exhausted':
        return { icon: <AlertCircle className="w-4 h-4" />, variant: 'error' as const, label: statusLabel(t, status) };
      case 'failed_internal':
        return { icon: <AlertCircle className="w-4 h-4" />, variant: 'error' as const, label: statusLabel(t, status) };
      case 'canceled':
        return { icon: <XCircle className="w-4 h-4" />, variant: 'default' as const, label: statusLabel(t, status) };
      default: 
        return { icon: <Info className="w-4 h-4" />, variant: 'default' as const, label: status };
    }
  };

  const display = getStatusDisplay(build.status);
  const startedDate = new Date(build.started_at * 1000).toLocaleString(dateLocale(locale));
  const duration = build.finished_at ? Math.max(0, build.finished_at - build.started_at) : null;
  const canCancel = build.status === 'pending' || build.status === 'running';
  const canDelete = !canCancel;
  const canViewLogs = terminalBuildStatuses.has(build.status);

  return (
    <div className={cn("grid gap-4 p-4 items-center hover:bg-white/20 dark:hover:bg-slate-800/30 transition-colors border-b border-white/20 dark:border-slate-700/50 last:border-0", tableLayouts.builds.grid, tableLayouts.builds.minWidth)}>
      <div className="min-w-0">
        <h3 className="font-semibold text-sm truncate" title={build.id}>{build.id}</h3>
        <p className="text-xs text-slate-500 dark:text-slate-400">{t('images.buildId')}</p>
      </div>

      <div className="min-w-0">
        <Badge variant={display.variant} className="flex items-center gap-1.5 px-2.5 py-1 w-fit">
          {display.icon}
          <span>{display.label}</span>
        </Badge>
      </div>

      <div className="min-w-0 text-sm text-slate-600 dark:text-slate-300">
        {duration !== null ? t('images.seconds', { value: duration }) : '-'}
      </div>

      <div className="min-w-0 text-sm text-slate-600 dark:text-slate-300 truncate" title={startedDate}>
        {startedDate}
      </div>

      <div className="flex items-center gap-2 justify-end shrink-0">
        <button
          onClick={() => onViewLogs(build)}
          disabled={!canViewLogs}
          className="p-2 rounded-lg bg-blue-100 text-blue-700 hover:bg-blue-200 dark:bg-blue-900/30 dark:text-blue-400 dark:hover:bg-blue-900/50 disabled:opacity-50 disabled:pointer-events-none transition-colors"
          title={canViewLogs ? t('images.viewLogs') : t('images.logsAfterFinish')}
        >
          <Terminal className="w-4 h-4" />
        </button>

        <button
          onClick={() => setConfirmAction('cancel')}
          disabled={!canCancel || cancelMutation.isPending}
          className="p-2 rounded-lg bg-yellow-100 text-yellow-700 hover:bg-yellow-200 dark:bg-yellow-900/30 dark:text-yellow-400 dark:hover:bg-yellow-900/50 disabled:opacity-50 transition-colors"
          title={t('images.cancelBuildRequested')}
        >
          <XCircle className="w-4 h-4" />
        </button>

        <button
          onClick={() => setConfirmAction('delete')}
          disabled={deleteMutation.isPending || !canDelete}
          className="p-2 rounded-lg bg-red-100 text-red-700 hover:bg-red-200 dark:bg-red-900/30 dark:text-red-400 dark:hover:bg-red-900/50 disabled:opacity-50 transition-colors ml-2"
          title={t('common.delete')}
        >
          <Trash2 className="w-4 h-4" />
        </button>
        <ConfirmDialog
          isOpen={confirmAction !== null}
          title={t('confirm.title')}
          message={confirmAction === 'cancel' ? t('images.cancelBuildConfirm', { id: build.id }) : t('images.deleteBuildConfirm', { id: build.id })}
          confirmLabel={confirmAction === 'cancel' ? t('projects.cancelRequested') : t('common.delete')}
          isLoading={cancelMutation.isPending || deleteMutation.isPending}
          variant={confirmAction === 'cancel' ? 'secondary' : 'danger'}
          onCancel={() => setConfirmAction(null)}
          onConfirm={() => {
            if (confirmAction === 'cancel') {
              cancelMutation.mutate(build.id, { onSuccess: () => setConfirmAction(null) });
            } else if (confirmAction === 'delete') {
              deleteMutation.mutate(build.id, { onSuccess: () => setConfirmAction(null) });
            }
          }}
        />
      </div>
    </div>
  );
}
