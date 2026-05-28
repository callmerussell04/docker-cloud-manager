import { Trash2, HardDrive, AlertTriangle } from 'lucide-react';
import { type VolumeData } from '../types';
import { tableLayouts } from '@/components/ui/tableLayouts';
import { cn } from '@/lib/utils';
import { dateLocale, statusLabel, useLocale, useT } from '@/lib/i18n';
import { useDeleteVolume } from '../hooks';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { useState } from 'react';

interface VolumeRowProps {
  volume: VolumeData;
}

export function VolumeRow({ volume }: VolumeRowProps) {
  const t = useT();
  const locale = useLocale();
  const [isConfirmOpen, setIsConfirmOpen] = useState(false);
  const deleteMutation = useDeleteVolume();

  const formattedDate = new Date(volume.created_at * 1000).toLocaleDateString(dateLocale(locale), {
    day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit'
  });
  
  const displayName = volume.name || `volume-${volume.id.slice(0, 8)}`;
  const isMissing = volume.status === 'missing';

  return (
    <div className={cn("grid gap-4 p-4 items-center hover:bg-white/20 dark:hover:bg-slate-800/30 transition-colors border-b border-white/20 dark:border-slate-700/50 last:border-0", tableLayouts.volumes.grid, tableLayouts.volumes.minWidth)}>
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
          {statusLabel(t, volume.status)}
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
          onClick={() => setIsConfirmOpen(true)}
          disabled={deleteMutation.isPending}
          className="p-2 rounded-lg bg-red-100 text-red-700 hover:bg-red-200 dark:bg-red-900/30 dark:text-red-400 dark:hover:bg-red-900/50 disabled:opacity-50 transition-colors"
          title={t('common.delete')}
        >
          <Trash2 className="w-4 h-4" />
        </button>
        <ConfirmDialog
          isOpen={isConfirmOpen}
          title={t('confirm.title')}
          message={t('volumes.deleteConfirm', { name: displayName })}
          confirmLabel={t('common.delete')}
          isLoading={deleteMutation.isPending}
          onCancel={() => setIsConfirmOpen(false)}
          onConfirm={() => deleteMutation.mutate(volume.id, { onSuccess: () => setIsConfirmOpen(false) })}
        />
      </div>
    </div>
  );
}
