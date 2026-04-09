import { Play, Square, Trash2, Globe, ExternalLink, Box, Activity } from 'lucide-react';
import { Badge } from '@/components/ui/Badge';
import { type ContainerData } from '../types';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { actionContainerFn } from '../api';
import { useToastStore } from '@/store/toastStore';
import { BASE_DOMAIN } from '@/config';
import { Link } from 'react-router-dom';

interface ContainerRowProps {
  container: ContainerData;
  onExpose: (container: ContainerData) => void;
}

export function ContainerRow({ container, onExpose }: ContainerRowProps) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);

  const actionMutation = useMutation({
    mutationFn: actionContainerFn,
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: ['containers'] });
      addToast(`Команда ${variables.action} успешно отправлена`, 'success');
    },
    onError: () => {
      addToast('Ошибка при выполнении действия', 'error');
    },
  });

  const getStatusBadge = (status: string) => {
    switch (status) {
      case 'running': return <Badge variant="success">Запущен</Badge>;
      case 'exited': return <Badge variant="default">Остановлен</Badge>;
      case 'creating': return <Badge variant="warning">Создается</Badge>;
      case 'error': return <Badge variant="error">Ошибка</Badge>;
      default: return <Badge variant="info">{status}</Badge>;
    }
  };

  const handleAction = (action: 'start' | 'stop' | 'delete') => {
    actionMutation.mutate({ id: container.id, action });
  };

  return (
    <div className="grid grid-cols-[2fr_1fr_1.5fr_auto] gap-4 p-4 items-center hover:bg-white/20 dark:hover:bg-slate-800/30 transition-colors border-b border-white/20 dark:border-slate-700/50 last:border-0 min-w-[900px]">
      <div className="flex items-center gap-3 min-w-0">
        <div className="w-10 h-10 rounded-xl bg-indigo-100 dark:bg-indigo-900/30 text-indigo-600 dark:text-indigo-400 flex items-center justify-center shrink-0">
          <Box className="w-5 h-5" />
        </div>
        <div className="min-w-0 flex-1">
          <Link to={`/containers/${container.id}`} state={{ container }} className="font-semibold text-base truncate hover:underline text-slate-900 dark:text-slate-100" title={container.name}>
            {container.name}
          </Link>
          <p className="text-xs text-slate-500 dark:text-slate-400 truncate" title={container.image_tag}>
            {container.image_tag}
          </p>
        </div>
      </div>

      <div className="min-w-0">
        {getStatusBadge(container.status)}
      </div>

      <div className="min-w-0 flex items-center text-sm text-slate-600 dark:text-slate-300">
        {container.domain_prefix ? (
          <a 
            href={`http://${container.domain_prefix}.${BASE_DOMAIN}`}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-2 text-indigo-600 dark:text-indigo-400 hover:text-indigo-700 dark:hover:text-indigo-300 transition-colors truncate max-w-full"
            title={`${container.domain_prefix}.${BASE_DOMAIN}:${container.internal_port}`}
          >
            <Globe className="w-4 h-4 shrink-0" />
            <span className="truncate">{container.domain_prefix}.{BASE_DOMAIN}</span>
            <ExternalLink className="w-3 h-3 shrink-0 opacity-70" />
          </a>
        ) : (
          <div className="flex items-center gap-2 text-slate-400 truncate" title="Не маршрутизируется">
            <Globe className="w-4 h-4 shrink-0" />
            <span className="truncate">Не маршрутизируется</span>
          </div>
        )}
      </div>

      <div className="flex items-center gap-2 justify-end shrink-0">
        <Link
          to={`/containers/${container.id}`}
          state={{ container }}
          className="p-2 rounded-lg bg-indigo-100 text-indigo-700 hover:bg-indigo-200 dark:bg-indigo-900/30 dark:text-indigo-400 dark:hover:bg-indigo-900/50 transition-colors"
          title="Статистика"
        >
          <Activity className="w-4 h-4" />
        </Link>

        <button
          onClick={() => handleAction('start')}
          disabled={container.status === 'running' || actionMutation.isPending}
          className="p-2 rounded-lg bg-green-100 text-green-700 hover:bg-green-200 dark:bg-green-900/30 dark:text-green-400 dark:hover:bg-green-900/50 disabled:opacity-50 transition-colors ml-2"
          title="Запустить"
        >
          <Play className="w-4 h-4" />
        </button>
        
        <button
          onClick={() => handleAction('stop')}
          disabled={container.status !== 'running' || actionMutation.isPending}
          className="p-2 rounded-lg bg-yellow-100 text-yellow-700 hover:bg-yellow-200 dark:bg-yellow-900/30 dark:text-yellow-400 dark:hover:bg-yellow-900/50 disabled:opacity-50 transition-colors"
          title="Остановить"
        >
          <Square className="w-4 h-4" />
        </button>

        <button
          onClick={() => onExpose(container)}
          disabled={actionMutation.isPending}
          className="p-2 rounded-lg bg-blue-100 text-blue-700 hover:bg-blue-200 dark:bg-blue-900/30 dark:text-blue-400 dark:hover:bg-blue-900/50 disabled:opacity-50 transition-colors"
          title="Настройки маршрутизации"
        >
          <Globe className="w-4 h-4" />
        </button>

        <button
          onClick={() => handleAction('delete')}
          disabled={actionMutation.isPending}
          className="p-2 rounded-lg bg-red-100 text-red-700 hover:bg-red-200 dark:bg-red-900/30 dark:text-red-400 dark:hover:bg-red-900/50 disabled:opacity-50 transition-colors ml-2"
          title="Удалить"
        >
          <Trash2 className="w-4 h-4" />
        </button>
      </div>
    </div>
  );
}