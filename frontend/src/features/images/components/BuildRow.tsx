import { Terminal, Trash2, Clock, CheckCircle2, AlertCircle, Loader2, Info } from 'lucide-react';
import { Badge } from '@/components/ui/Badge';
import { type BuildData } from '../types';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { deleteBuildFn } from '../api';
import { useToastStore } from '@/store/toastStore';

interface BuildRowProps {
  build: BuildData;
  onViewLogs: (build: BuildData) => void;
}

export function BuildRow({ build, onViewLogs }: BuildRowProps) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);

  const deleteMutation = useMutation({
    mutationFn: deleteBuildFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['builds'] });
      addToast('Запись о сборке удалена', 'success');
    },
    onError: () => {
      addToast('Ошибка при удалении записи', 'error');
    },
  });

  const getStatusDisplay = (status: string) => {
    switch (status) {
      case 'success': 
        return { icon: <CheckCircle2 className="w-4 h-4" />, variant: 'success' as const, label: 'Успешно' };
      case 'running': 
        return { icon: <Loader2 className="w-4 h-4 animate-spin" />, variant: 'info' as const, label: 'Собирается' };
      case 'pending': 
        return { icon: <Clock className="w-4 h-4" />, variant: 'warning' as const, label: 'В очереди' };
      case 'failed': 
        return { icon: <AlertCircle className="w-4 h-4" />, variant: 'error' as const, label: 'Ошибка' };
      case 'failed_timeout': 
        return { icon: <AlertCircle className="w-4 h-4" />, variant: 'error' as const, label: 'Таймаут' };
      case 'failed_quota_exceeded': 
        return { icon: <AlertCircle className="w-4 h-4" />, variant: 'error' as const, label: 'Квота превышена' };
      default: 
        return { icon: <Info className="w-4 h-4" />, variant: 'default' as const, label: status };
    }
  };

  const display = getStatusDisplay(build.status);
  const startedDate = new Date(build.started_at * 1000).toLocaleString('ru-RU');
  const duration = build.finished_at ? Math.max(0, build.finished_at - build.started_at) : null;

  return (
    <div className="grid grid-cols-[1.5fr_1fr_1fr_1.5fr_auto] gap-4 p-4 items-center hover:bg-white/20 dark:hover:bg-slate-800/30 transition-colors border-b border-white/20 dark:border-slate-700/50 last:border-0 min-w-[800px]">
      <div className="min-w-0">
        <h3 className="font-semibold text-sm truncate" title={build.id}>{build.id}</h3>
        <p className="text-xs text-slate-500 dark:text-slate-400">ID сборки</p>
      </div>

      <div className="min-w-0">
        <Badge variant={display.variant} className="flex items-center gap-1.5 px-2.5 py-1 w-fit">
          {display.icon}
          <span>{display.label}</span>
        </Badge>
      </div>

      <div className="min-w-0 text-sm text-slate-600 dark:text-slate-300">
        {duration !== null ? `${duration} сек` : '-'}
      </div>

      <div className="min-w-0 text-sm text-slate-600 dark:text-slate-300 truncate" title={startedDate}>
        {startedDate}
      </div>

      <div className="flex items-center gap-2 justify-end shrink-0">
        <button
          onClick={() => onViewLogs(build)}
          className="p-2 rounded-lg bg-blue-100 text-blue-700 hover:bg-blue-200 dark:bg-blue-900/30 dark:text-blue-400 dark:hover:bg-blue-900/50 transition-colors"
          title="Просмотр логов"
        >
          <Terminal className="w-4 h-4" />
        </button>

        <button
          onClick={() => deleteMutation.mutate(build.id)}
          disabled={deleteMutation.isPending || build.status === 'running'}
          className="p-2 rounded-lg bg-red-100 text-red-700 hover:bg-red-200 dark:bg-red-900/30 dark:text-red-400 dark:hover:bg-red-900/50 disabled:opacity-50 transition-colors ml-2"
          title="Удалить запись"
        >
          <Trash2 className="w-4 h-4" />
        </button>
      </div>
    </div>
  );
}