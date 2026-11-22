import { Activity, Box, Cpu, HardDrive, Layers, Disc, Database, Server, RefreshCcw } from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/Card';
import { formatBytes } from '@/shared/lib/utils';
import { useT } from '@/shared/i18n';
import { ErrorState } from '@/shared/ui/ErrorState';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { failureReason } from '@/shared/api/apiError';
import { useDashboardStats } from '@/features/dashboard/queries';
import { millicoresToCores } from '@/features/dashboard/model';
import { Page } from '@/shared/ui/Page';
import { Toolbar, ToolbarButton } from '@/shared/ui/Toolbar';

export function DashboardPage() {
  const t = useT();
  const { data: stats, isLoading, isError, error, refetch, isFetching } = useDashboardStats();

  const getProgressColor = (percent: number) => {
    if (percent < 70) return 'bg-accent-fill';
    if (percent < 90) return 'bg-orange';
    return 'bg-red';
  };

  const renderProgressBar = (used: number, total: number, formatFn?: (val: number) => string) => {
    const safeUsed = used || 0;
    const safeTotal = total || 0;
    const percent = safeTotal > 0 ? (safeUsed / safeTotal) * 100 : 0;
    const formattedUsed = formatFn ? formatFn(safeUsed) : safeUsed;
    const formattedTotal = formatFn ? formatFn(safeTotal) : safeTotal;

    return (
      <div className="space-y-3 mt-4">
        <div className="flex justify-between items-end">
          <span className="text-3xl font-semibold">{formattedUsed}</span>
          <span className="text-sm text-slate-500 dark:text-slate-400 mb-1">/ {formattedTotal}</span>
        </div>
        <div className="w-full bg-slate-200/50 dark:bg-slate-800 rounded-full h-3 border border-slate-200 dark:border-slate-700 overflow-hidden">
          <div 
            className={`h-full rounded-full transition-all duration-1000 ease-out ${getProgressColor(percent)}`} 
            style={{ width: `${Math.min(percent, 100)}%` }} 
          />
        </div>
        <div className="text-right text-xs text-slate-500">
          {t('dashboard.usedPercent', { percent: percent.toFixed(1) })}
        </div>
      </div>
    );
  };

  const safeStats = {
    ram_used_bytes: stats?.ram_used_bytes || 0,
    ram_quota_bytes: stats?.ram_quota_bytes || 0,
    cpu_used_millicores: stats?.cpu_used_millicores || 0,
    cpu_quota_millicores: stats?.cpu_quota_millicores || 0,
    disk_used_mb: stats?.disk_used_mb || 0,
    disk_quota_mb: stats?.disk_quota_mb || 0,
    containers_total: stats?.containers_total || 0,
    containers_quota: stats?.containers_quota || 0,
    volumes_total: stats?.volumes_total || 0,
    volumes_quota: stats?.volumes_quota || 0,
    containers_running: stats?.containers_running || 0,
    images_total: stats?.images_total || 0,
    projects_total: stats?.projects_total || 0,
  };

  return (
    <Page
      toolbar={
        <Toolbar
          title={t('dashboard.title')}
          actions={<ToolbarButton icon={RefreshCcw} label={t('common.refresh')} busy={isFetching} onClick={() => refetch()} />}
        />
      }
    >

      {isLoading ? (
        <LoadingRows rows={6} dot={false} columns={[1, 2]} />
      ) : isError ? (
        <ErrorState
          title={t('dashboard.loadFailed')}
          message={failureReason(error, t)}
          onRetry={() => refetch()}
          isRetrying={isFetching}
        />
      ) : stats ? (
        <>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-lg flex items-center gap-2">
                  <Server className="w-5 h-5 text-label-2" />
                  {t('dashboard.ram')}
                </CardTitle>
              </CardHeader>
              <CardContent>
                {renderProgressBar(safeStats.ram_used_bytes, safeStats.ram_quota_bytes, formatBytes)}
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-lg flex items-center gap-2">
                  <Cpu className="w-5 h-5 text-label-2" />
                  {t('dashboard.cpu')}
                </CardTitle>
              </CardHeader>
              <CardContent>
                {renderProgressBar(safeStats.cpu_used_millicores, safeStats.cpu_quota_millicores, (val) => t('dashboard.cores', { cores: millicoresToCores(val) }))}
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-lg flex items-center gap-2">
                  <Database className="w-5 h-5 text-label-2" />
                  {t('dashboard.disk')}
                </CardTitle>
              </CardHeader>
              <CardContent>
                {renderProgressBar(safeStats.disk_used_mb, safeStats.disk_quota_mb, (val) => `${val} MB`)}
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-lg flex items-center gap-2">
                  <Box className="w-5 h-5 text-label-2" />
                  {t('dashboard.containerLimit')}
                </CardTitle>
              </CardHeader>
              <CardContent>
                {renderProgressBar(safeStats.containers_total, safeStats.containers_quota)}
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-lg flex items-center gap-2">
                  <HardDrive className="w-5 h-5 text-label-2" />
                  {t('dashboard.volumeLimit')}
                </CardTitle>
              </CardHeader>
              <CardContent>
                {renderProgressBar(safeStats.volumes_total, safeStats.volumes_quota)}
              </CardContent>
            </Card>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-3 gap-6 pt-4">
            <Card>
              <CardContent className="p-6 flex items-center gap-4">
                <div className="w-12 h-12 rounded-xl bg-fill flex items-center justify-center shrink-0">
                  <Activity className="w-6 h-6 text-label-2" />
                </div>
                <div>
                  <p className="text-sm font-medium text-slate-500 dark:text-slate-400">{t('dashboard.runningContainers')}</p>
                  <h3 className="text-2xl font-bold text-slate-900 dark:text-slate-100">{safeStats.containers_running}</h3>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardContent className="p-6 flex items-center gap-4">
                <div className="w-12 h-12 rounded-xl bg-fill flex items-center justify-center shrink-0">
                  <Disc className="w-6 h-6 text-label-2" />
                </div>
                <div>
                  <p className="text-sm font-medium text-slate-500 dark:text-slate-400">{t('dashboard.customImages')}</p>
                  <h3 className="text-2xl font-bold text-slate-900 dark:text-slate-100">{safeStats.images_total}</h3>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardContent className="p-6 flex items-center gap-4">
                <div className="w-12 h-12 rounded-xl bg-fill flex items-center justify-center shrink-0">
                  <Layers className="w-6 h-6 text-label-2" />
                </div>
                <div>
                  <p className="text-sm font-medium text-slate-500 dark:text-slate-400">{t('dashboard.composeProjects')}</p>
                  <h3 className="text-2xl font-bold text-slate-900 dark:text-slate-100">{safeStats.projects_total}</h3>
                </div>
              </CardContent>
            </Card>
          </div>
        </>
      ) : (
        <ErrorState title={t('dashboard.loadFailed')} onRetry={() => refetch()} isRetrying={isFetching} />
      )}
    </Page>
  );
}
