import { Layers, Square, Trash2, AlertTriangle, Loader2, Play, XCircle } from 'lucide-react';
import { Badge } from '@/components/ui/Badge';
import { type ProjectData } from '../types';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { cancelProjectFn, deleteProjectFn, stopProjectFn, startProjectFn } from '../api';
import { useToastStore } from '@/store/toastStore';
import { getApiErrorMessage } from '@/lib/apiError';
import { tableLayouts } from '@/components/ui/tableLayouts';
import { cn } from '@/lib/utils';

interface ProjectRowProps {
  project: ProjectData;
}

export function ProjectRow({ project }: ProjectRowProps) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);

  const deleteMutation = useMutation({
    mutationFn: deleteProjectFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      addToast('Проект успешно удален', 'success');
    },
    onError: (error: unknown) => {
      const { message, requestId } = getApiErrorMessage(error, 'Не удалось удалить проект');
      addToast(message, 'error', { requestId });
    },
  });

  const startMutation = useMutation({
    mutationFn: startProjectFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      addToast('Команда запуска отправлена', 'success');
    },
    onError: (error: unknown) => {
      const { message, requestId } = getApiErrorMessage(error, 'Не удалось запустить проект');
      addToast(message, 'error', { requestId });
    },
  });

  const stopMutation = useMutation({
    mutationFn: stopProjectFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      addToast('Команда остановки отправлена', 'success');
    },
    onError: (error: unknown) => {
      const { message, requestId } = getApiErrorMessage(error, 'Не удалось остановить проект');
      addToast(message, 'error', { requestId });
    },
  });

  const cancelMutation = useMutation({
    mutationFn: cancelProjectFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      queryClient.invalidateQueries({ queryKey: ['builds'] });
      addToast('Отмена проекта запрошена', 'success');
    },
    onError: (error: unknown) => {
      const { message, requestId } = getApiErrorMessage(error, 'Не удалось отменить проект');
      addToast(message, 'error', { requestId });
    },
  });

  const getStatusBadge = (status: string) => {
    switch (status) {
      case 'running': return <Badge variant="success">Запущен</Badge>;
      case 'stopped': return <Badge variant="default">Остановлен</Badge>;
      case 'building': return <Badge variant="warning" className="flex gap-1.5"><Loader2 className="w-3 h-3 animate-spin"/>Сборка</Badge>;
      case 'deploying': return <Badge variant="info" className="flex gap-1.5"><Loader2 className="w-3 h-3 animate-spin"/>Развертывание</Badge>;
      case 'canceling': return <Badge variant="warning" className="flex gap-1.5"><Loader2 className="w-3 h-3 animate-spin"/>Отмена</Badge>;
      case 'canceled': return <Badge variant="default">Отменен</Badge>;
      case 'starting': return <Badge variant="info" className="flex gap-1.5"><Loader2 className="w-3 h-3 animate-spin"/>Запуск</Badge>;
      case 'stopping': return <Badge variant="warning" className="flex gap-1.5"><Loader2 className="w-3 h-3 animate-spin"/>Остановка</Badge>;
      case 'deleting': return <Badge variant="warning">Удаляется</Badge>;
      case 'pending': return <Badge variant="default">В очереди</Badge>;
      case 'failed': return <Badge variant="error">Ошибка</Badge>;
      default: return <Badge variant="default">{status}</Badge>;
    }
  };

  const formattedDate = new Date(project.created_at * 1000).toLocaleDateString('ru-RU', {
    day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit'
  });

  const isWorking = ['building', 'deploying', 'canceling', 'pending', 'starting', 'stopping', 'deleting'].includes(project.status);
  const canCancel = project.status === 'building' || project.status === 'deploying';

  return (
    <div className={cn("grid gap-4 p-4 items-center hover:bg-white/20 dark:hover:bg-slate-800/30 transition-colors border-b border-white/20 dark:border-slate-700/50 last:border-0 relative", tableLayouts.projects.grid, tableLayouts.projects.minWidth)}>
      {project.status === 'failed' && (
        <div className="absolute top-0 left-0 w-1 h-full bg-red-500" />
      )}
      
      <div className="flex items-center gap-3 min-w-0 pl-1">
        <div className="w-10 h-10 rounded-xl bg-indigo-100 dark:bg-indigo-900/30 text-indigo-600 dark:text-indigo-400 flex items-center justify-center shrink-0">
          <Layers className="w-5 h-5" />
        </div>
        <div className="min-w-0 flex-1">
          <h3 className="font-semibold text-base truncate" title={project.name}>{project.name}</h3>
        </div>
      </div>

      <div className="min-w-0">
        {getStatusBadge(project.status)}
      </div>

      <div className="min-w-0 flex items-center">
        {project.status === 'failed' && (project.error_message || project.last_error) ? (
          <div 
            className="flex items-center gap-2 text-sm text-red-600 dark:text-red-400 truncate max-w-full cursor-help"
            title={project.error_message || project.last_error}
          >
            <AlertTriangle className="w-4 h-4 shrink-0" />
            <span className="truncate">{project.error_message || project.last_error}</span>
          </div>
        ) : (
          <span className="text-slate-400 dark:text-slate-500">-</span>
        )}
      </div>

      <div className="min-w-0 text-sm text-slate-500 dark:text-slate-400 truncate" title={formattedDate}>
        {formattedDate}
      </div>

      <div className="flex items-center gap-2 justify-end shrink-0">
        <button
          onClick={() => startMutation.mutate(project.id)}
          disabled={startMutation.isPending || isWorking || project.status === 'running'}
          className="p-2 rounded-lg bg-green-100 text-green-700 hover:bg-green-200 dark:bg-green-900/30 dark:text-green-400 dark:hover:bg-green-900/50 disabled:opacity-50 transition-colors"
          title="Запустить сервисы"
        >
          <Play className="w-4 h-4" />
        </button>

        <button
          onClick={() => stopMutation.mutate(project.id)}
          disabled={stopMutation.isPending || isWorking || project.status === 'failed' || project.status === 'stopped'}
          className="p-2 rounded-lg bg-yellow-100 text-yellow-700 hover:bg-yellow-200 dark:bg-yellow-900/30 dark:text-yellow-400 dark:hover:bg-yellow-900/50 disabled:opacity-50 transition-colors"
          title="Остановить сервисы"
        >
          <Square className="w-4 h-4" />
        </button>

        <button
          onClick={() => {
            if (window.confirm(`Отменить развертывание проекта "${project.name}"?`)) {
              cancelMutation.mutate(project.id);
            }
          }}
          disabled={!canCancel || cancelMutation.isPending}
          className="p-2 rounded-lg bg-orange-100 text-orange-700 hover:bg-orange-200 dark:bg-orange-900/30 dark:text-orange-400 dark:hover:bg-orange-900/50 disabled:opacity-50 transition-colors"
          title="Отменить развертывание"
        >
          <XCircle className="w-4 h-4" />
        </button>

        <button
          onClick={() => {
            if (window.confirm(`Удалить проект "${project.name}"?`)) {
              deleteMutation.mutate(project.id);
            }
          }}
          disabled={deleteMutation.isPending || isWorking}
          className="p-2 rounded-lg bg-red-100 text-red-700 hover:bg-red-200 dark:bg-red-900/30 dark:text-red-400 dark:hover:bg-red-900/50 disabled:opacity-50 transition-colors ml-2"
          title="Удалить проект"
        >
          <Trash2 className="w-4 h-4" />
        </button>
      </div>
    </div>
  );
}
