import { Trash2, HardDrive } from 'lucide-react';
import { Card, CardContent } from '@/components/ui/Card';
import { Badge } from '@/components/ui/Badge';
import { type VolumeData } from '../types';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { deleteVolumeFn } from '../api';
import { useToastStore } from '@/store/toastStore';

interface VolumeCardProps {
  volume: VolumeData;
}

export function VolumeCard({ volume }: VolumeCardProps) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);

  const deleteMutation = useMutation({
    mutationFn: deleteVolumeFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['volumes'] });
      addToast('Том успешно удален', 'success');
    },
    onError: () => {
      addToast('Ошибка удаления. Возможно, том используется контейнером.', 'error');
    },
  });

  const formattedDate = new Date(volume.created_at * 1000).toLocaleDateString('ru-RU', {
    day: '2-digit', month: 'short', year: 'numeric'
  });

  return (
    <Card className="flex flex-col">
      <CardContent className="p-6 flex-1 flex flex-col">
        <div className="flex items-start gap-3 mb-4">
          <div className="w-10 h-10 rounded-xl bg-indigo-100 dark:bg-indigo-900/30 text-indigo-600 dark:text-indigo-400 flex items-center justify-center shrink-0">
            <HardDrive className="w-5 h-5" />
          </div>
          <div className="overflow-hidden">
            <h3 className="font-semibold text-lg truncate" title={volume.docker_name}>
              {volume.docker_name.split('_').slice(2).join('_') || volume.docker_name}
            </h3>
            <p className="text-xs text-slate-500 dark:text-slate-400 font-mono truncate">
              {volume.docker_name}
            </p>
          </div>
        </div>

        <div className="flex justify-between items-center mb-6 text-sm flex-1">
          <Badge variant="info">Driver: {volume.driver || 'local'}</Badge>
          <span className="text-slate-500 dark:text-slate-400">{formattedDate}</span>
        </div>

        <div className="flex items-center gap-2 mt-auto pt-4 border-t border-slate-200 dark:border-slate-700/50">
          <button
            onClick={() => deleteMutation.mutate(volume.id)}
            disabled={deleteMutation.isPending}
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