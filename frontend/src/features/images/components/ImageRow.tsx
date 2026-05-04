import { Trash2, Disc, AlertTriangle } from 'lucide-react';
import { Badge } from '@/components/ui/Badge';
import { type ImageData } from '../types';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { deleteImageFn } from '../api';
import { useToastStore } from '@/store/toastStore';
import { getApiErrorMessage } from '@/lib/apiError';
import { tableLayouts } from '@/components/ui/tableLayouts';
import { cn } from '@/lib/utils';

interface ImageRowProps {
  image: ImageData;
}

export function ImageRow({ image }: ImageRowProps) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);

  const deleteMutation = useMutation({
    mutationFn: deleteImageFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['images'] });
      addToast('Образ успешно удален', 'success');
    },
    onError: (error: unknown) => {
      const { message, requestId } = getApiErrorMessage(error, 'Не удалось удалить образ');
      addToast(message, 'error', { requestId });
    },
  });

  const formattedDate = new Date(image.created_at * 1000).toLocaleDateString('ru-RU', {
    day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit'
  });

  return (
    <div className={cn("grid gap-4 p-4 items-center hover:bg-white/20 dark:hover:bg-slate-800/30 transition-colors border-b border-white/20 dark:border-slate-700/50 last:border-0", tableLayouts.images.grid, tableLayouts.images.minWidth)}>
      <div className="flex items-center gap-3 min-w-0">
        <div className="w-10 h-10 rounded-xl bg-indigo-100 dark:bg-indigo-900/30 text-indigo-600 dark:text-indigo-400 flex items-center justify-center shrink-0">
          <Disc className="w-5 h-5" />
        </div>
        <div className="min-w-0 flex-1">
          <h3 className="font-semibold text-base truncate" title={image.tag}>{image.tag}</h3>
        </div>
      </div>

      <div className="min-w-0 text-sm">
        <Badge variant="info">{image.size_mb} MB</Badge>
      </div>

      <div className="min-w-0 text-sm">
        <Badge variant={image.status === 'available' ? 'success' : image.status === 'error' || image.status === 'missing' ? 'error' : 'default'}>
          {image.status}
        </Badge>
      </div>

      <div className="min-w-0 text-sm text-slate-500 dark:text-slate-400 truncate" title={formattedDate}>
        {formattedDate}
        {image.last_error && (
          <div className="flex items-center gap-1 text-xs text-red-600 dark:text-red-400 truncate" title={image.last_error}>
            <AlertTriangle className="w-3 h-3 shrink-0" />
            <span className="truncate">{image.last_error}</span>
          </div>
        )}
      </div>

      <div className="flex items-center gap-2 justify-end shrink-0">
        <button
          onClick={() => {
            if (window.confirm(`Удалить образ "${image.tag}"?`)) {
              deleteMutation.mutate(image.id);
            }
          }}
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
