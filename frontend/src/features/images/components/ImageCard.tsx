import { Trash2, Box } from 'lucide-react';
import { Card, CardContent } from '@/components/ui/Card';
import { Badge } from '@/components/ui/Badge';
import type { ImageData } from '../types';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { deleteImageFn } from '../api';
import { useToastStore } from '@/store/toastStore';

interface ImageCardProps {
  image: ImageData;
}

export function ImageCard({ image }: ImageCardProps) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);

  const deleteMutation = useMutation({
    mutationFn: deleteImageFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['images'] });
      addToast('Образ успешно удален', 'success');
    },
    onError: () => {
      addToast('Ошибка при удалении образа. Возможно он используется.', 'error');
    },
  });

  const formattedDate = new Date(image.created_at * 1000).toLocaleDateString('ru-RU', {
    day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit'
  });

  return (
    <Card className="flex flex-col">
      <CardContent className="p-6 flex-1 flex flex-col">
        <div className="flex justify-between items-start mb-4">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-xl bg-indigo-100 dark:bg-indigo-900/30 text-indigo-600 dark:text-indigo-400 flex items-center justify-center shrink-0">
              <Box className="w-5 h-5" />
            </div>
            <div className="overflow-hidden">
              <h3 className="font-semibold text-lg truncate" title={image.tag}>{image.tag}</h3>
              <p className="text-sm text-slate-500 dark:text-slate-400 truncate">{formattedDate}</p>
            </div>
          </div>
        </div>

        <div className="flex flex-wrap gap-2 mb-4 flex-1">
          <Badge variant="info">{image.size_mb} MB</Badge>
          {image.is_custom ? (
            <Badge variant="warning">Кастомный</Badge>
          ) : (
            <Badge variant="default">Системный</Badge>
          )}
        </div>

        {image.is_custom && (
          <div className="flex items-center gap-2 mt-auto pt-4 border-t border-slate-200 dark:border-slate-700/50">
            <button
              onClick={() => deleteMutation.mutate(image.id)}
              disabled={deleteMutation.isPending}
              className="p-2 rounded-lg bg-red-100 text-red-700 hover:bg-red-200 dark:bg-red-900/30 dark:text-red-400 dark:hover:bg-red-900/50 disabled:opacity-50 transition-colors ml-auto"
              title="Удалить"
            >
              <Trash2 className="w-4 h-4" />
            </button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}