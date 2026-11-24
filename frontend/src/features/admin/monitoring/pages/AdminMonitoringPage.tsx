import { useState, type ReactNode } from 'react';

import { failureReason } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import { formatBytes } from '@/shared/lib/utils';
import { ErrorState } from '@/shared/ui/ErrorState';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { Page } from '@/shared/ui/Page';
import { StatusDot } from '@/shared/ui/StatusDot';
import { Toolbar } from '@/shared/ui/Toolbar';
import { millicoresToCores } from '@/features/admin/settings/model';
import { useSystemConfig } from '@/features/admin/settings/queries';
import { CapacityPanel } from '../components/CapacityPanel';
import { HealthList } from '../components/HealthList';
import { HostCounts } from '../components/HostCounts';
import { VitalChart } from '../components/VitalChart';
import { appendVitalSample, cpuCapacity, diskCapacity, memoryCapacity, type SystemMonitoring, type VitalSample, withDefaults } from '../model';
import { useSystemMonitoring } from '../queries';

// useVitals keeps the samples taken while the page is open, starting empty:
// one per answer the monitoring poll brings, none from an answer cached by
// an earlier visit.
function useVitals(stats: SystemMonitoring | undefined, updatedAt: number) {
  const [record, setRecord] = useState<{ at: number; samples: VitalSample[] }>(() => ({ at: updatedAt, samples: [] }));
  // Adjusting state while rendering, as React documents for state that
  // follows a changed input; React re-renders before committing.
  if (stats && updatedAt !== record.at) {
    setRecord({ at: updatedAt, samples: appendVitalSample(record.samples, { cpu: stats.cpu_percent, memory: stats.memory_used_bytes }) });
  }
  return record.samples;
}

// The page's rows, each as many columns as fit at their least width. The
// classes are written out whole so Tailwind finds them.
const grids = {
  charts: 'grid gap-x-8 gap-y-6 [grid-template-columns:repeat(auto-fit,minmax(min(100%,280px),1fr))]',
  panels: 'grid gap-x-8 gap-y-6 [grid-template-columns:repeat(auto-fit,minmax(min(100%,240px),1fr))]',
  sections: 'grid gap-x-8 gap-y-6 [grid-template-columns:repeat(auto-fit,minmax(min(100%,320px),1fr))]',
};

function Section({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  return (
    <section aria-labelledby={id} className="grid min-w-0 content-start gap-3">
      <h2 id={id} className="text-[17px] font-semibold tracking-[-0.01em] text-label">
        {title}
      </h2>
      {children}
    </section>
  );
}

// AdminMonitoringPage is the host at a glance: live CPU and memory charts
// sampled every 2 s while the page is open, then memory, CPU and disk as
// capacity bars of what holds each, then the health checks and the host's
// counts.
export function AdminMonitoringPage() {
  const t = useT();
  const { data, dataUpdatedAt, isLoading, isError, isFetching, error, refetch } = useSystemMonitoring();
  const config = useSystemConfig();
  const samples = useVitals(data, dataUpdatedAt);

  const cores = (millicores: number) => {
    const value = Number(millicoresToCores(millicores).toFixed(1));
    return value === 1 ? t('admin.monitoring.oneCore') : t('admin.monitoring.cores', { value });
  };
  const bytes = (value: number) => formatBytes(value, 1);

  let content: ReactNode;
  if (data) {
    const stats = withDefaults(data);
    const memory = memoryCapacity(stats, config.data?.reserved_system_memory_bytes);
    const cpu = cpuCapacity(stats, config.data?.reserved_system_cpu_millicores);
    const disk = diskCapacity(stats);
    content = (
      <>
        <p className="text-label-2">{t('admin.monitoring.liveHint')}</p>
        <div className={grids.charts}>
          <VitalChart
            label={t('admin.monitoring.cpuBusy')}
            value={`${stats.cpu_percent.toFixed(1)}%`}
            series={samples.map((sample) => sample.cpu)}
            max={100}
          />
          <VitalChart
            label={t('admin.monitoring.memoryInUse')}
            value={bytes(stats.memory_used_bytes)}
            sub={t('admin.monitoring.of', { total: bytes(stats.memory_total_bytes) })}
            series={samples.map((sample) => sample.memory)}
            max={stats.memory_total_bytes}
          />
        </div>
        <div className={grids.panels}>
          <CapacityPanel
            title={t('admin.monitoring.memoryReserved')}
            headline={bytes(memory.total - memory.free)}
            sub={t('admin.monitoring.of', { total: bytes(memory.total) })}
            capacity={memory}
            format={bytes}
          />
          <CapacityPanel
            title={t('admin.monitoring.cpuReserved')}
            headline={cores(cpu.total - cpu.free)}
            sub={t('admin.monitoring.of', { total: cores(cpu.total) })}
            capacity={cpu}
            format={cores}
          />
          <CapacityPanel
            title={t('admin.monitoring.disk')}
            headline={bytes(disk.total - disk.free)}
            sub={t('admin.monitoring.of', { total: bytes(disk.total) })}
            capacity={disk}
            format={bytes}
          />
        </div>
        <div className={grids.sections}>
          <Section id="monitoring-health" title={t('admin.monitoring.health')}>
            <HealthList stats={stats} />
          </Section>
          <Section id="monitoring-counts" title={t('admin.monitoring.onHost')}>
            <HostCounts stats={stats} />
          </Section>
        </div>
      </>
    );
  } else if (isLoading) {
    content = <LoadingRows rows={6} dot={false} columns={[1, 1]} />;
  } else {
    content = <ErrorState title={t('admin.monitoring.loadFailed')} message={failureReason(error, t)} onRetry={() => refetch()} isRetrying={isFetching} />;
  }

  // A reading keeps its figures on screen when a later poll fails; the
  // status then says the page is not updating.
  const status = data ? (
    <StatusDot tone={isError ? 'idle' : 'ok'}>{isError ? t('admin.monitoring.notUpdating') : t('admin.monitoring.live')}</StatusDot>
  ) : undefined;

  return <Page toolbar={<Toolbar title={t('admin.monitoring.title')} status={status} />}>{content}</Page>;
}
