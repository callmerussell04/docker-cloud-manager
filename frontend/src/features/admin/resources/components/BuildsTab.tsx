import { useState } from 'react';
import { ScrollText, Trash2, XCircle } from 'lucide-react';

import { Badge } from '@/shared/ui/Badge';
import { Button } from '@/shared/ui/Button';
import { getApiErrorMessage } from '@/shared/api/apiError';
import { cn } from '@/shared/lib/utils';
import { useLocale, useT } from '@/shared/i18n';
import { BuildLogsModal } from '@/features/images/components/BuildLogsModal';
import { buildLogsAvailable, buildStatusLabel, isBuildActive, type AdminBuildData } from '@/features/images/model';
import { useBuilds, useCancelBuild, useDeleteBuild } from '@/features/images/queries';
import { formatDateTime, ownerName, resourceRowClass, statusVariant } from './resourceDisplay';
import { ResourceTable } from './ResourceTable';
import { ResourceConfirmDialog, type ConfirmRequest } from './ResourceConfirmDialog';

// Grid classes shared by this table's header and rows.
const layout = {
  minWidth: 'min-w-[1180px]',
  grid: 'grid-cols-[minmax(180px,1.4fr)_minmax(130px,0.9fr)_minmax(130px,1fr)_minmax(180px,1.3fr)_minmax(170px,1.2fr)_minmax(100px,0.8fr)_10rem]',
} as const;

const pageSize = 20;

function buildDurationSeconds(startedAt: number, finishedAt: number) {
  if (!startedAt || !finishedAt) return null;
  return Math.max(0, finishedAt - startedAt);
}

export function BuildsTab() {
  const t = useT();
  const locale = useLocale();
  const [page, setPage] = useState(1);
  const [viewLogsBuild, setViewLogsBuild] = useState<AdminBuildData | null>(null);
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null);
  const { data, isLoading, isFetching, isError, error, refetch } = useBuilds('all', page, pageSize);
  const deleteBuild = useDeleteBuild('all');
  const cancelBuild = useCancelBuild('all');

  return (
    <>
      <ResourceTable
        layout={layout}
        columns={(
          <>
            <div className="pl-2">{t('admin.resources.buildId')}</div>
            <div>{t('common.status')}</div>
            <div>{t('admin.resources.imageId')}</div>
            <div>{t('common.user')}</div>
            <div>{t('images.startedAt')}</div>
            <div>{t('images.duration')}</div>
            <div className="flex justify-end">{t('admin.users.management')}</div>
          </>
        )}
        isLoading={isLoading}
        isFetching={isFetching}
        error={isError ? { title: t('images.loadBuildsFailed'), message: getApiErrorMessage(error, t('images.loadBuildsFailed'), t).message, onRetry: () => refetch() } : null}
        empty={{ title: t('images.emptyBuilds'), description: t('images.emptyBuildsDescription') }}
        itemCount={data?.items.length ?? 0}
        page={page}
        pageSize={pageSize}
        totalItems={data?.total_count}
        onPageChange={setPage}
      >
        {data?.items.map((build) => {
          const canCancel = isBuildActive(build.status);
          const canViewLogs = buildLogsAvailable(build.status);
          const duration = buildDurationSeconds(build.started_at, build.finished_at);
          return (
            <div key={build.id} className={cn(resourceRowClass, layout.grid)}>
              <div className="min-w-0 pr-4 pl-2">
                <div className="font-medium block truncate font-mono" title={build.id}>{build.id}</div>
              </div>
              <div className="min-w-0">
                <Badge variant={statusVariant(build.status)}>{buildStatusLabel(t, build.status)}</Badge>
              </div>
              <div className="min-w-0 text-xs font-mono text-slate-500 truncate" title={build.image_id || build.tag}>{build.tag || build.image_id || '-'}</div>
              <div className="min-w-0 text-xs font-mono text-slate-500 truncate" title={build.owner_username || build.owner_id}>{t('admin.resources.userLabel', { value: ownerName(build) })}</div>
              <div className="min-w-0 text-xs text-slate-500 truncate" title={formatDateTime(build.started_at, locale)}>{formatDateTime(build.started_at, locale)}</div>
              <div className="min-w-0 text-sm text-slate-600 dark:text-slate-300">
                {duration !== null ? t('images.seconds', { value: duration }) : '-'}
              </div>
              <div className="flex gap-2 justify-end shrink-0">
                <Button
                  variant="secondary"
                  className="h-8 px-2"
                  disabled={!canViewLogs}
                  title={canViewLogs ? t('images.viewLogs') : t('images.logsAfterFinish')} aria-label={canViewLogs ? t('images.viewLogs') : t('images.logsAfterFinish')}
                  onClick={() => setViewLogsBuild(build)}
                >
                  <ScrollText className="w-4 h-4" />
                </Button>
                <Button title={t('images.cancelBuild')} aria-label={t('images.cancelBuild')}
                  variant="secondary"
                  className="h-8 px-2"
                  disabled={!canCancel || cancelBuild.isPending}
                  onClick={() => setConfirm({ title: t('images.cancelBuildConfirm', { id: build.id }), run: () => cancelBuild.mutate(build.id), confirmLabel: t('images.cancelBuild'), variant: 'primary' })}
                >
                  <XCircle className="w-4 h-4 text-yellow-500" />
                </Button>
                <Button title={t('common.delete')} aria-label={t('common.delete')}
                  variant="danger"
                  className="h-8 px-2"
                  disabled={canCancel || deleteBuild.isPending}
                  onClick={() => setConfirm({ title: t('images.deleteBuildConfirm', { id: build.id }), run: () => deleteBuild.mutate(build.id) })}
                >
                  <Trash2 className="w-4 h-4" />
                </Button>
              </div>
            </div>
          );
        })}
      </ResourceTable>

      <BuildLogsModal build={viewLogsBuild} scope="all" onClose={() => setViewLogsBuild(null)} />
      <ResourceConfirmDialog request={confirm} isLoading={deleteBuild.isPending || cancelBuild.isPending} onClose={() => setConfirm(null)} />
    </>
  );
}
