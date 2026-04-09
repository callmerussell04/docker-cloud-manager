/* eslint-disable @typescript-eslint/no-explicit-any */
import { Play, Square, Trash2, Globe, ExternalLink, Activity } from 'lucide-react';
import { Card, CardContent } from '@/components/ui/Card';
import { Badge } from '@/components/ui/Badge';
import { type ContainerData } from '../types';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { actionContainerFn } from '../api';
import { useToastStore } from '@/store/toastStore';
import { BASE_DOMAIN } from '@/config';
import { Link } from 'react-router-dom';

interface ContainerCardProps {
  container: ContainerData;
  onExpose: (container: ContainerData) => void;
}

export function ContainerCard({ container, onExpose }: ContainerCardProps) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);

  const actionMutation = useMutation({
    mutationFn: actionContainerFn,
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: ['containers'] });
      queryClient.invalidateQueries({ queryKey: ['admin_containers'] });
      addToast(`Команда ${variables.action} успешно отправлена`, 'success');
    },
    onError: (error: any) => {
      addToast(error.response?.data?.error || 'Ошибка при выполнении действия', 'error');
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
    <Card className="flex flex-col min-w-0">
      <CardContent className="p-6 flex-1 flex flex-col min-w-0">
        <div className="flex justify-between items-start mb-4 gap-4">
          <div className="flex-1 min-w-0">
            <Link to={`/containers/${container.id}`} state={{ container }} className="font-semibold text-lg hover:underline block truncate text-slate-900 dark:text-slate-100" title={container.name}>
              {container.name}
            </Link>
            <p className="text-sm text-slate-500 dark:text-slate-400 truncate block" title={container.image_tag}>
              {container.image_tag}
            </p>
          </div>
          <div className="shrink-0">
            {getStatusBadge(container.status)}
          </div>
        </div>

        <div className="space-y-2 flex-1 text-sm text-slate-600 dark:text-slate-300 min-w-0">
          {container.domain_prefix ? (
            <a 
              href={`http://${container.domain_prefix}.${BASE_DOMAIN}`}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-2 text-indigo-600 dark:text-indigo-400 bg-indigo-50 dark:bg-indigo-900/20 px-2 py-1 rounded-md border border-indigo-100 dark:border-indigo-800 hover:bg-indigo-100 dark:hover:bg-indigo-900/40 transition-colors w-full"
              title={`${container.domain_prefix}.${BASE_DOMAIN}:${container.internal_port}`}
            >
              <Globe className="w-4 h-4 shrink-0" />
              <span className="truncate">{container.domain_prefix}.{BASE_DOMAIN}</span>
              <ExternalLink className="w-3 h-3 ml-auto opacity-70 shrink-0" />
            </a>
          ) : (
            <div className="flex items-center gap-2 text-slate-400 px-2 py-1 truncate" title="Не маршрутизируется">
              <Globe className="w-4 h-4 shrink-0" />
              <span className="truncate">Не маршрутизируется</span>
            </div>
          )}
        </div>

        <div className="flex items-center gap-2 mt-6 pt-4 border-t border-slate-200 dark:border-slate-700/50">
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
            className="p-2 rounded-lg bg-green-100 text-green-700 hover:bg-green-200 dark:bg-green-900/30 dark:text-green-400 dark:hover:bg-green-900/50 disabled:opacity-50 transition-colors"
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
            className="p-2 rounded-lg bg-red-100 text-red-700 hover:bg-red-200 dark:bg-red-900/30 dark:text-red-400 dark:hover:bg-red-900/50 disabled:opacity-50 transition-colors ml-auto"
            title="Удалить"
          >
            <Trash2 className="w-4 h-4" />
          </button>
        </div>
      </CardContent>
    </Card>
  );
}