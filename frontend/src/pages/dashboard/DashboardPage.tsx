import { useQuery } from '@tanstack/react-query';
import { Activity, Box, HardDrive, Layers, Disc, Database, Server, RefreshCcw } from 'lucide-react';
import { getDashboardStatsFn } from '@/features/dashboard/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { formatBytes } from '@/lib/utils';
import { Button } from '@/components/ui/Button';
import { useT } from '@/lib/i18n';
import { queryKeys } from '@/shared/api/queryKeys';
import { ErrorState } from '@/components/common/ErrorState';
import { getApiErrorMessage } from '@/lib/apiError';

export function DashboardPage() {
  const t = useT();
  const { data: stats, isLoading, isError, error, refetch, isFetching } = useQuery({
    queryKey: queryKeys.dashboard.stats,
    queryFn: getDashboardStatsFn,
  });

  const getProgressColor = (percent: number) => {
    if (percent < 70) return 'bg-indigo-500';
    if (percent < 90) return 'bg-yellow-500';
    return 'bg-red-500';
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
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">{t('dashboard.title')}</h1>
          <p className="text-slate-500 dark:text-slate-400 mt-1">{t('dashboard.subtitle')}</p>
        </div>
        <Button variant="secondary" onClick={() => refetch()} isLoading={isFetching} className="px-3">
          <RefreshCcw className="w-4 h-4 mr-2" /> {t('common.refresh')}
        </Button>
      </div>

      {isLoading ? (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          {[...Array(4)].map((_, i) => (
            <div key={i} className="h-48 bg-white/40 dark:bg-slate-900/40 rounded-2xl animate-pulse" />
          ))}
        </div>
      ) : isError ? (
        <ErrorState
          title={t('dashboard.loadFailed')}
          message={getApiErrorMessage(error, t('dashboard.loadFailed'), t).message}
          onRetry={() => refetch()}
          isRetrying={isFetching}
        />
      ) : stats ? (
        <>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-lg flex items-center gap-2">
                  <Server className="w-5 h-5 text-indigo-500" />
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
                  <Database className="w-5 h-5 text-emerald-500" />
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
                  <Box className="w-5 h-5 text-blue-500" />
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
                  <HardDrive className="w-5 h-5 text-purple-500" />
                  {t('dashboard.volumeLimit')}
                </CardTitle>
              </CardHeader>
              <CardContent>
                {renderProgressBar(safeStats.volumes_total, safeStats.volumes_quota)}
              </CardContent>
            </Card>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-3 gap-6 pt-4">
            <Card className="bg-indigo-50/50 dark:bg-indigo-950/20 border-indigo-100 dark:border-indigo-900/50">
              <CardContent className="p-6 flex items-center gap-4">
                <div className="w-12 h-12 rounded-xl bg-indigo-100 dark:bg-indigo-900/50 flex items-center justify-center shrink-0">
                  <Activity className="w-6 h-6 text-indigo-600 dark:text-indigo-400" />
                </div>
                <div>
                  <p className="text-sm font-medium text-slate-500 dark:text-slate-400">{t('dashboard.runningContainers')}</p>
                  <h3 className="text-2xl font-bold text-slate-900 dark:text-slate-100">{safeStats.containers_running}</h3>
                </div>
              </CardContent>
            </Card>

            <Card className="bg-emerald-50/50 dark:bg-emerald-950/20 border-emerald-100 dark:border-emerald-900/50">
              <CardContent className="p-6 flex items-center gap-4">
                <div className="w-12 h-12 rounded-xl bg-emerald-100 dark:bg-emerald-900/50 flex items-center justify-center shrink-0">
                  <Disc className="w-6 h-6 text-emerald-600 dark:text-emerald-400" />
                </div>
                <div>
                  <p className="text-sm font-medium text-slate-500 dark:text-slate-400">{t('dashboard.customImages')}</p>
                  <h3 className="text-2xl font-bold text-slate-900 dark:text-slate-100">{safeStats.images_total}</h3>
                </div>
              </CardContent>
            </Card>

            <Card className="bg-amber-50/50 dark:bg-amber-950/20 border-amber-100 dark:border-amber-900/50">
              <CardContent className="p-6 flex items-center gap-4">
                <div className="w-12 h-12 rounded-xl bg-amber-100 dark:bg-amber-900/50 flex items-center justify-center shrink-0">
                  <Layers className="w-6 h-6 text-amber-600 dark:text-amber-400" />
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
        <div className="text-center p-8 text-slate-500">{t('dashboard.loadFailed')}</div>
      )}
    </div>
  );
}
