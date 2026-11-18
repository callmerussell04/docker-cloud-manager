import { Activity, AlertTriangle, Box, Cpu, Database, Disc, HardDrive, Layers, RefreshCcw, Server } from 'lucide-react';

import { Button } from '@/shared/ui/Button';
import { dateLocale, useLocale, useT } from '@/shared/i18n';
import { formatBytes } from '@/shared/lib/utils';
import { buildsHeldByDisk, withDefaults } from '../model';
import { useSystemMonitoring } from '../queries';
import { MetricCard } from '../components/MetricCard';
import { CountCard } from '../components/CountCard';
import { RootfsQuotaCard, VolumeSizeModeCard } from '../components/HostStatusCards';

function admissionReasonLabel(t: ReturnType<typeof useT>, reason: string) {
  switch (reason) {
    case 'host_memory_exhausted':
      return t('admin.monitoring.reason.host_memory_exhausted');
    case 'host_disk_floor':
      return t('admin.monitoring.reason.host_disk_floor');
    default:
      return reason;
  }
}

export function AdminMonitoringPage() {
  const t = useT();
  const locale = useLocale();
  const { data, isLoading, isFetching, refetch } = useSystemMonitoring();
  const stats = withDefaults(data);
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
          {stats.admission_status === 'blocked' && (
            <div className="flex items-start gap-3 rounded-lg border border-red-200 bg-red-50 p-4 text-red-800 dark:border-red-900/70 dark:bg-red-950/40 dark:text-red-200">
              <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0" />
              <div>
                <p className="font-semibold">{t('admin.monitoring.admissionBlocked')}</p>
                <p className="mt-1 text-sm">{stats.admission_reasons.map((reason) => admissionReasonLabel(t, reason)).join(', ')}</p>
              </div>
            </div>
          )}
          {buildsHeldByDisk(stats) && (
            <div className="flex items-start gap-3 rounded-lg border border-amber-200 bg-amber-50 p-4 text-amber-900 dark:border-amber-900/70 dark:bg-amber-950/40 dark:text-amber-200">
              <HardDrive className="mt-0.5 h-5 w-5 shrink-0" />
              <div>
                <p className="font-semibold">
                  {t('admin.monitoring.buildsHeldByDisk', { cap: stats.build_claim_cap, max: stats.max_concurrent_builds })}
                </p>
                <p className="mt-1 text-sm">{t('admin.monitoring.buildsHeldByDiskDetail')}</p>
              </div>
            </div>
          )}
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
              detail={`${formatBytes(stats.disk_free_bytes)} ${t('admin.monitoring.free')} / ${formatBytes(stats.host_min_free_disk_bytes)} ${t('admin.monitoring.floor')}`}
            />
            <MetricCard
              title={t('admin.monitoring.dcmDisk')}
              icon={Database}
              used={stats.dcm_disk_used_bytes}
              total={stats.disk_total_bytes}
              detail={t('admin.monitoring.ofHostDisk')}
            />
            <MetricCard
              title={t('admin.monitoring.reservedVolumes')}
              icon={HardDrive}
              used={stats.dcm_reserved_volume_bytes}
              total={stats.disk_total_bytes}
              detail={t('admin.monitoring.reservedVolumesDetail')}
            />
            <MetricCard
              title={t('admin.monitoring.unusedVolumePromise')}
              icon={HardDrive}
              used={stats.dcm_unused_volume_promise_bytes}
              total={stats.disk_free_bytes}
              detail={t('admin.monitoring.unusedVolumePromiseDetail')}
            />
            <MetricCard
              title={t('admin.monitoring.buildMemory')}
              icon={Server}
              used={stats.dcm_reserved_build_memory_bytes}
              total={stats.memory_total_bytes}
              detail={t('admin.monitoring.ofHostMemory')}
            />
            <MetricCard
              title={t('admin.monitoring.borrowedMemory')}
              icon={Server}
              used={stats.dcm_borrowed_memory_bytes}
              total={stats.memory_total_bytes}
              detail={t('admin.monitoring.borrowedMemoryDetail')}
            />
          </div>

          <RootfsQuotaCard status={stats.rootfs_quota_status} />
          <VolumeSizeModeCard mode={stats.volume_size_mode} />

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
