import { Trash2, HardDrive, AlertTriangle } from 'lucide-react';
import { type VolumeData } from '../types';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { deleteVolumeFn } from '../api';
import { useToastStore } from '@/store/toastStore';

interface VolumeRowProps {
  volume: VolumeData;
}

export function VolumeRow({ volume }: VolumeRowProps) {
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
    day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit'
  });
  
  const displayName = `volume-${volume.id.slice(0, 8)}`;
  const isMissing = volume.status === 'missing';

  return (
    <div className="grid grid-cols-[2fr_1fr_1.5fr_auto] gap-4 p-4 items-center hover:bg-white/20 dark:hover:bg-slate-800/30 transition-colors border-b border-white/20 dark:border-slate-700/50 last:border-0 min-w-[700px]">
      <div className="flex items-center gap-3 min-w-0">
        <div className="w-10 h-10 rounded-xl bg-indigo-100 dark:bg-indigo-900/30 text-indigo-600 dark:text-indigo-400 flex items-center justify-center shrink-0">
          <HardDrive className="w-5 h-5" />
        </div>
        <div className="min-w-0 flex-1">
          <h3 className="font-semibold text-base truncate" title={displayName}>{displayName}</h3>
          <p className="text-xs text-slate-500 dark:text-slate-400 font-mono truncate" title={volume.id}>
            {volume.id}
          </p>
        </div>
      </div>

      <div className="min-w-0 text-sm">
        <span className={isMissing ? 'text-red-600 dark:text-red-400' : 'text-slate-600 dark:text-slate-300'}>
          {volume.status}
        </span>
      </div>

      <div className="min-w-0 text-sm text-slate-500 dark:text-slate-400 truncate" title={formattedDate}>
        {formattedDate}
        {volume.last_error && (
          <div className="flex items-center gap-1 text-xs text-red-600 dark:text-red-400 truncate" title={volume.last_error}>
            <AlertTriangle className="w-3 h-3 shrink-0" />
            <span className="truncate">{volume.last_error}</span>
          </div>
        )}
      </div>

      <div className="flex items-center gap-2 justify-end shrink-0">
        <button
          onClick={() => deleteMutation.mutate(volume.id)}
          disabled={deleteMutation.isPending}
          className="p-2 rounded-lg bg-red-100 text-red-700 hover:bg-red-200 dark:bg-red-900/30 dark:text-red-400 dark:hover:bg-red-900/50 disabled:opacity-50 transition-colors"
          title="Удалить"
        >
          <Trash2 className="w-4 h-4" />
        </button>
      </div>
    </div>
  );
}
