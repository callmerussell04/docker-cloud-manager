import { Activity, Box, Cpu, Database, Disc, HardDrive, Layers, RefreshCcw, Server, type LucideIcon } from 'lucide-react';

import { Button } from '@/components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import type { SystemMonitoring } from '@/features/admin/types';
import { dateLocale, useLocale, useT } from '@/lib/i18n';
import { formatBytes } from '@/lib/utils';
import { useSystemMonitoring } from '@/features/admin/hooks';

function progressColor(percent: number) {
  if (percent < 70) return 'bg-emerald-500';
  if (percent < 90) return 'bg-yellow-500';
  return 'bg-red-500';
}

function MetricCard({
  title,
  icon: Icon,
  used,
  total,
  value,
  detail,
  format = formatBytes,
}: {
  title: string;
  icon: LucideIcon;
  used: number;
  total: number;
  value?: string;
  detail?: string;
  format?: (value: number) => string;
}) {
  const percent = total > 0 ? (used / total) * 100 : 0;
  const displayValue = value ?? format(used);
  const displayDetail = detail ?? `/ ${format(total)}`;

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-lg flex items-center gap-2">
          <Icon className="w-5 h-5 text-indigo-500" />
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-3">
          <div className="flex justify-between items-end gap-3">
            <span className="text-3xl font-semibold text-slate-900 dark:text-slate-100">{displayValue}</span>
            <span className="text-sm text-slate-500 dark:text-slate-400 mb-1 text-right">{displayDetail}</span>
          </div>
          <div className="w-full bg-slate-200/50 dark:bg-slate-800 rounded-full h-3 border border-slate-200 dark:border-slate-700 overflow-hidden">
            <div
              className={`h-full rounded-full transition-all duration-700 ease-out ${progressColor(percent)}`}
              style={{ width: `${Math.min(percent, 100)}%` }}
            />
          </div>
          <div className="text-right text-xs text-slate-500">{percent.toFixed(1)}%</div>
        </div>
      </CardContent>
    </Card>
  );
}

function CountCard({ label, value, icon: Icon }: { label: string; value: number; icon: LucideIcon }) {
  return (
    <Card>
      <CardContent className="p-5 flex items-center gap-4">
        <div className="w-11 h-11 rounded-xl bg-slate-100 dark:bg-slate-800 flex items-center justify-center shrink-0">
          <Icon className="w-5 h-5 text-slate-700 dark:text-slate-200" />
        </div>
        <div className="min-w-0">
          <p className="text-sm font-medium text-slate-500 dark:text-slate-400 truncate">{label}</p>
          <p className="text-2xl font-bold text-slate-900 dark:text-slate-100">{value}</p>
        </div>
      </CardContent>
    </Card>
  );
}

function safeStats(stats?: SystemMonitoring): SystemMonitoring {
  return {
    cpu_percent: stats?.cpu_percent ?? 0,
    memory_total_bytes: stats?.memory_total_bytes ?? 0,
    memory_used_bytes: stats?.memory_used_bytes ?? 0,
    memory_available_bytes: stats?.memory_available_bytes ?? 0,
    disk_total_bytes: stats?.disk_total_bytes ?? 0,
    disk_used_bytes: stats?.disk_used_bytes ?? 0,
    disk_free_bytes: stats?.disk_free_bytes ?? 0,
    dcm_reserved_memory_bytes: stats?.dcm_reserved_memory_bytes ?? 0,
    dcm_disk_used_bytes: stats?.dcm_disk_used_bytes ?? 0,
    containers_total: stats?.containers_total ?? 0,
    containers_running: stats?.containers_running ?? 0,
    containers_stopped: stats?.containers_stopped ?? 0,
    containers_error: stats?.containers_error ?? 0,
    containers_missing: stats?.containers_missing ?? 0,
    volumes_total: stats?.volumes_total ?? 0,
    images_total: stats?.images_total ?? 0,
    builds_total: stats?.builds_total ?? 0,
    projects_total: stats?.projects_total ?? 0,
    observed_at: stats?.observed_at ?? 0,
  };
}

export function AdminMonitoringPage() {
  const t = useT();
  const locale = useLocale();
  const { data, isLoading, isFetching, refetch } = useSystemMonitoring();
  const stats = safeStats(data);
  const observedAt = stats.observed_at
    ? new Date(stats.observed_at * 1000).toLocaleString(dateLocale(locale))
    : t('status.unknown');

  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-red-600 dark:text-red-400 flex items-center gap-3">
            <Activity className="w-8 h-8" />
            {t('admin.monitoring.title')}
          </h1>
          <p className="text-slate-500 dark:text-slate-400 mt-1">{t('admin.monitoring.subtitle')}</p>
          <p className="text-xs text-slate-500 dark:text-slate-500 mt-2">
            {t('admin.monitoring.observedAt', { value: observedAt })}
          </p>
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
      ) : data ? (
        <>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <MetricCard
              title={t('admin.monitoring.cpu')}
              icon={Cpu}
              used={stats.cpu_percent}
              total={100}
              value={`${stats.cpu_percent.toFixed(1)}%`}
              detail={t('admin.monitoring.currentLoad')}
              format={(value) => `${value.toFixed(1)}%`}
            />
            <MetricCard
              title={t('admin.monitoring.memory')}
              icon={Server}
              used={stats.memory_used_bytes}
              total={stats.memory_total_bytes}
              detail={`${formatBytes(stats.memory_available_bytes)} ${t('admin.monitoring.available')}`}
            />
            <MetricCard
              title={t('admin.monitoring.hostDisk')}
              icon={HardDrive}
              used={stats.disk_used_bytes}
              total={stats.disk_total_bytes}
              detail={`${formatBytes(stats.disk_free_bytes)} ${t('admin.monitoring.free')}`}
            />
            <MetricCard
              title={t('admin.monitoring.dcmDisk')}
              icon={Database}
              used={stats.dcm_disk_used_bytes}
              total={stats.disk_total_bytes}
              detail={t('admin.monitoring.ofHostDisk')}
            />
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-4">
            <CountCard label={t('admin.monitoring.containersTotal')} value={stats.containers_total} icon={Box} />
            <CountCard label={t('admin.monitoring.containersRunning')} value={stats.containers_running} icon={Activity} />
            <CountCard label={t('admin.monitoring.volumesTotal')} value={stats.volumes_total} icon={HardDrive} />
            <CountCard label={t('admin.monitoring.imagesTotal')} value={stats.images_total} icon={Disc} />
            <CountCard label={t('admin.monitoring.buildsTotal')} value={stats.builds_total} icon={Database} />
            <CountCard label={t('admin.monitoring.projectsTotal')} value={stats.projects_total} icon={Layers} />
            <CountCard label={t('admin.monitoring.containersStopped')} value={stats.containers_stopped} icon={Box} />
            <CountCard label={t('admin.monitoring.containersProblem')} value={stats.containers_error + stats.containers_missing} icon={Activity} />
          </div>
        </>
      ) : (
        <div className="text-center p-8 text-slate-500">{t('admin.monitoring.loadFailed')}</div>
      )}
    </div>
  );
}
