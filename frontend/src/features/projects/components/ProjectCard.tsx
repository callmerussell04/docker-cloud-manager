import { Layers, Square, Trash2, AlertTriangle, Loader2 } from 'lucide-react';
import { Card, CardContent } from '@/components/ui/Card';
import { Badge } from '@/components/ui/Badge';
import { type ProjectData } from '../types';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { deleteProjectFn, stopProjectFn } from '../api';
import { useToastStore } from '@/store/toastStore';

interface ProjectCardProps {
  project: ProjectData;
}

export function ProjectCard({ project }: ProjectCardProps) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);

  const deleteMutation = useMutation({
    mutationFn: deleteProjectFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      addToast('Проект успешно удален', 'success');
    },
    onError: () => {
      addToast('Ошибка при удалении проекта', 'error');
    },
  });

  const stopMutation = useMutation({
    mutationFn: stopProjectFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      addToast('Команда остановки отправлена', 'success');
    },
    onError: () => {
      addToast('Ошибка при остановке проекта', 'error');
    },
  });

  const getStatusBadge = (status: string) => {
    switch (status) {
      case 'running': return <Badge variant="success">Запущен</Badge>;
      case 'building': return <Badge variant="warning" className="flex gap-1.5"><Loader2 className="w-3 h-3 animate-spin"/>Сборка</Badge>;
      case 'deploying': return <Badge variant="info" className="flex gap-1.5"><Loader2 className="w-3 h-3 animate-spin"/>Развертывание</Badge>;
      case 'pending': return <Badge variant="default">В очереди</Badge>;
      case 'failed': return <Badge variant="error">Ошибка</Badge>;
      default: return <Badge variant="default">{status}</Badge>;
    }
  };

  const formattedDate = new Date(project.created_at * 1000).toLocaleDateString('ru-RU', {
    day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit'
  });

  const isWorking = project.status === 'building' || project.status === 'deploying' || project.status === 'pending';

  return (
    <Card className="flex flex-col relative overflow-hidden">
      {project.status === 'failed' && (
        <div className="absolute top-0 left-0 w-1 h-full bg-red-500" />
      )}
      <CardContent className="p-6 flex-1 flex flex-col">
        <div className="flex justify-between items-start mb-4">
          <div className="flex items-start gap-3">
            <div className="w-10 h-10 rounded-xl bg-indigo-100 dark:bg-indigo-900/30 text-indigo-600 dark:text-indigo-400 flex items-center justify-center shrink-0">
              <Layers className="w-5 h-5" />
            </div>
            <div>
              <h3 className="font-semibold text-lg truncate max-w-[150px]">{project.name}</h3>
              <p className="text-xs text-slate-500 dark:text-slate-400">{formattedDate}</p>
            </div>
          </div>
          {getStatusBadge(project.status)}
        </div>

        <div className="flex-1 text-sm">
          {project.status === 'failed' && project.error_message && (
            <div className="bg-red-50 dark:bg-red-900/20 text-red-800 dark:text-red-300 p-3 rounded-lg border border-red-100 dark:border-red-900/50 flex items-start gap-2">
              <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5" />
              <span className="text-xs line-clamp-3" title={project.error_message}>{project.error_message}</span>
            </div>
          )}
        </div>

        <div className="flex items-center gap-2 mt-4 pt-4 border-t border-slate-200 dark:border-slate-700/50">
          <button
            onClick={() => stopMutation.mutate(project.id)}
            disabled={stopMutation.isPending || isWorking || project.status === 'failed'}
            className="p-2 rounded-lg bg-yellow-100 text-yellow-700 hover:bg-yellow-200 dark:bg-yellow-900/30 dark:text-yellow-400 dark:hover:bg-yellow-900/50 disabled:opacity-50 transition-colors"
            title="Остановить сервисы"
          >
            <Square className="w-4 h-4" />
          </button>

          <button
            onClick={() => deleteMutation.mutate(project.id)}
            disabled={deleteMutation.isPending || isWorking}
            className="p-2 rounded-lg bg-red-100 text-red-700 hover:bg-red-200 dark:bg-red-900/30 dark:text-red-400 dark:hover:bg-red-900/50 disabled:opacity-50 transition-colors ml-auto"
            title="Удалить проект"
          >
            <Trash2 className="w-4 h-4" />
          </button>
        </div>
      </CardContent>
    </Card>
  );
}