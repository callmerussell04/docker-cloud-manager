import { useState } from 'react';
import { AlertTriangle, Disc, Trash2 } from 'lucide-react';

import { Badge } from '@/shared/ui/Badge';
import { Button } from '@/shared/ui/Button';
import { getApiErrorMessage } from '@/shared/api/apiError';
import { cn, formatBytes } from '@/shared/lib/utils';
import { statusLabel, useLocale, useT } from '@/shared/i18n';
import { useDeleteImage, useImages } from '@/features/images/queries';
import { adminImageTableLayout as layout } from './tableLayouts';
import { formatDateTime, ownerName, resourceRowClass, statusVariant } from './resourceDisplay';
import { ResourceTable } from './ResourceTable';
import { ResourceConfirmDialog, type ConfirmRequest } from './ResourceConfirmDialog';

const pageSize = 20;

export function ImagesTab() {
  const t = useT();
  const locale = useLocale();
  const [page, setPage] = useState(1);
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null);
  const { data, isFetching, isError, error, refetch } = useImages('all', page, pageSize);
  const deleteImage = useDeleteImage('all');

  return (
    <>
      <ResourceTable
        layout={layout}
        columns={(
          <>
            <div className="pl-2">{t('images.tag')}</div>
            <div>{t('images.size')}</div>
            <div>{t('common.status')}</div>
            <div>{t('images.createdAt')}</div>
            <div>{t('admin.resources.owner')}</div>
            <div className="flex justify-end">{t('admin.users.management')}</div>
          </>
        )}
        isFetching={isFetching}
        error={isError ? { title: t('images.loadFailed'), message: getApiErrorMessage(error, t('images.loadFailed'), t).message, onRetry: () => refetch() } : null}
        empty={{ icon: <Disc className="h-8 w-8" />, title: t('images.noImages'), description: t('images.noImagesDescription') }}
        itemCount={data?.items.length ?? 0}
        page={page}
        pageSize={pageSize}
        totalItems={data?.total_count}
        onPageChange={setPage}
      >
        {data?.items.map((img) => (
          <div key={img.id} className={cn(resourceRowClass, layout.grid)}>
            <div className="min-w-0 pr-4 pl-2">
              <div className="font-medium block truncate" title={img.tag}>{img.tag}</div>
            </div>
            <div className="min-w-0 text-sm">
              <Badge variant="info">{formatBytes(img.size_bytes)}</Badge>
            </div>
            <div className="min-w-0">
              <Badge variant={statusVariant(img.status)}>{statusLabel(t, img.status)}</Badge>
              {img.last_error && (
                <div className="mt-1 flex items-center gap-1 text-xs text-red-600 dark:text-red-400" title={img.last_error}>
                  <AlertTriangle className="h-3 w-3 shrink-0" />
                  <span className="truncate">{img.last_error}</span>
                </div>
              )}
            </div>
            <div className="min-w-0 text-sm text-slate-500 dark:text-slate-400 truncate" title={formatDateTime(img.created_at, locale)}>
              {formatDateTime(img.created_at, locale)}
            </div>
            <div className="min-w-0 text-xs font-mono text-slate-500 pr-4">
              <div className="block truncate" title={img.owner_username || img.owner_id}>{t('admin.resources.userLabel', { value: ownerName(img) })}</div>
            </div>
            <div className="flex gap-2 justify-end shrink-0">
              <Button
                variant="danger"
                className="h-8 px-2"
                disabled={deleteImage.isPending}
                onClick={() => setConfirm({ message: t('images.deleteConfirm', { name: img.tag }), run: () => deleteImage.mutate(img.id) })}
              >
                <Trash2 className="w-4 h-4" />
              </Button>
            </div>
          </div>
        ))}
      </ResourceTable>

      <ResourceConfirmDialog request={confirm} isLoading={deleteImage.isPending} onClose={() => setConfirm(null)} />
    </>
  );
}
