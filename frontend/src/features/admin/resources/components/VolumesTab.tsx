import { useState } from 'react';
import { AlertTriangle, HardDrive, Trash2 } from 'lucide-react';

import { Badge } from '@/shared/ui/Badge';
import { Button } from '@/shared/ui/Button';
import { getApiErrorMessage } from '@/shared/api/apiError';
import { cn } from '@/shared/lib/utils';
import { useLocale, useT } from '@/shared/i18n';
import { volumeStatusLabel } from '@/features/volumes/model';
import { useDeleteVolume, useVolumes } from '@/features/volumes/queries';
import { adminVolumeTableLayout as layout } from './tableLayouts';
import { formatDateTime, ownerName, resourceRowClass, statusVariant } from './resourceDisplay';
import { ResourceTable } from './ResourceTable';
import { ResourceConfirmDialog, type ConfirmRequest } from './ResourceConfirmDialog';

const pageSize = 20;

export function VolumesTab() {
  const t = useT();
  const locale = useLocale();
  const [page, setPage] = useState(1);
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null);
  const { data, isFetching, isError, error, refetch } = useVolumes('all', page, pageSize);
  const deleteVolume = useDeleteVolume('all');

  return (
    <>
      <ResourceTable
        layout={layout}
        columns={(
          <>
            <div className="pl-2">{t('volumes.name')}</div>
            <div>{t('common.status')}</div>
            <div>{t('images.createdAt')}</div>
            <div>{t('admin.resources.owner')}</div>
            <div className="flex justify-end">{t('admin.users.management')}</div>
          </>
        )}
        isFetching={isFetching}
        error={isError ? { title: t('volumes.loadFailed'), message: getApiErrorMessage(error, t('volumes.loadFailed'), t).message, onRetry: () => refetch() } : null}
        empty={{ icon: <HardDrive className="h-8 w-8" />, title: t('volumes.emptyTitle'), description: t('volumes.emptyDescription') }}
        itemCount={data?.items.length ?? 0}
        page={page}
        pageSize={pageSize}
        totalItems={data?.total_count}
        onPageChange={setPage}
      >
        {data?.items.map((v) => (
          <div key={v.id} className={cn(resourceRowClass, layout.grid)}>
            <div className="min-w-0 pr-4 pl-2">
              <div className="font-medium block truncate font-mono">{v.name || v.id}</div>
              <div className="text-xs text-slate-500 block truncate font-mono" title={v.docker_name}>{v.docker_name}</div>
              <div className="text-xs text-slate-500 block truncate font-mono">{v.id}</div>
            </div>
            <div className="min-w-0 text-sm">
              <Badge variant={statusVariant(v.status)}>{volumeStatusLabel(t, v.status)}</Badge>
              {v.last_error && (
                <div className="mt-1 flex items-center gap-1 text-xs text-red-600 dark:text-red-400" title={v.last_error}>
                  <AlertTriangle className="h-3 w-3 shrink-0" />
                  <span className="truncate">{v.last_error}</span>
                </div>
              )}
            </div>
            <div className="min-w-0 text-sm text-slate-500 dark:text-slate-400 truncate" title={formatDateTime(v.created_at, locale)}>
              {formatDateTime(v.created_at, locale)}
            </div>
            <div className="min-w-0 text-xs font-mono text-slate-500 pr-4">
              <div className="block truncate" title={v.owner_username || v.owner_id}>{t('admin.resources.userLabel', { value: ownerName(v) })}</div>
            </div>
            <div className="flex gap-2 justify-end shrink-0">
              <Button
                variant="danger"
                className="h-8 px-2"
                disabled={deleteVolume.isPending}
                onClick={() => setConfirm({ message: t('volumes.deleteConfirm', { name: v.name || v.id }), run: () => deleteVolume.mutate(v.id) })}
              >
                <Trash2 className="w-4 h-4" />
              </Button>
            </div>
          </div>
        ))}
      </ResourceTable>

      <ResourceConfirmDialog request={confirm} isLoading={deleteVolume.isPending} onClose={() => setConfirm(null)} />
    </>
  );
}
