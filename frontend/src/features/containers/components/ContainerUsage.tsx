import type { ReactNode } from 'react';
import { useT } from '@/shared/i18n';
import { formatBytes } from '@/shared/lib/utils';
import { ErrorState } from '@/shared/ui/ErrorState';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { Sparkline } from '@/shared/ui/Sparkline';
import { borrowedMemoryBytes, type ContainerStats, type UsageSample } from '../model';

interface ContainerUsageProps {
  running: boolean;
  stats?: ContainerStats;
  // history is what the page has sampled since it opened, oldest first.
  history: readonly UsageSample[];
  loading: boolean;
  // error is the reason the stats could not be read, with a retry.
  error?: { message?: string; retry: () => void; retrying: boolean };
}

function Metric({ label, value, unit, sub, children }: { label: string; value: string; unit?: string; sub?: ReactNode; children?: ReactNode }) {
  return (
    <div className="border-t border-separator py-3 first:border-t-0 first:pt-0">
      <div className="text-[12px] font-semibold text-label-2">{label}</div>
      <div className="mt-1 text-[24px] font-[650] tabular-nums tracking-[-0.02em]">
        {value}
        {unit && <span className="ml-0.5 text-[15px] font-[550] text-label-2">{unit}</span>}
      </div>
      {sub && <div className="text-[12px] text-label-2">{sub}</div>}
      {children}
    </div>
  );
}

// ContainerUsage is a running container's CPU, memory (with borrowed memory
// called out) and network. Its lines draw only what the page has sampled
// while open, so they start empty.
export function ContainerUsage({ running, stats, history, loading, error }: ContainerUsageProps) {
  const t = useT();
  if (!running) return <p className="text-label-2">{t('containers.usageNotRunning')}</p>;
  if (error) {
    return <ErrorState compact title={t('containers.statsLoadFailed')} message={error.message} onRetry={error.retry} isRetrying={error.retrying} />;
  }
  if (!stats) return loading ? <LoadingRows rows={3} dot={false} columns={[1]} /> : null;

  const borrowed = borrowedMemoryBytes(stats);
  const cpuSeries = history.map((sample) => sample.cpu);
  return (
    <div>
      <Metric label={t('containers.cpu')} value={stats.cpu_percentage.toFixed(1)} unit="%">
        <Sparkline series={cpuSeries} max={Math.max(100, ...cpuSeries)} />
      </Metric>
      <Metric
        label={t('containers.memory')}
        value={formatBytes(stats.memory_usage_bytes, 1)}
        sub={
          <>
            {t('containers.memoryOf', { limit: formatBytes(stats.memory_limit_bytes, 1) })}
            {borrowed > 0 && <span className="text-orange"> · {t('containers.memoryBorrowed', { amount: formatBytes(borrowed, 1) })}</span>}
          </>
        }
      >
        <Sparkline series={history.map((sample) => sample.memory)} max={stats.memory_limit_bytes} />
        {borrowed > 0 && <p className="mt-2 text-[12px] text-label-3">{t('containers.borrowedMemoryNote')}</p>}
      </Metric>
      <Metric
        label={t('containers.network')}
        value={formatBytes(stats.network_rx_bytes, 1)}
        sub={t('containers.networkSent', { tx: formatBytes(stats.network_tx_bytes, 1) })}
      />
    </div>
  );
}
