import { useT } from '@/shared/i18n';
import { Sparkline } from '@/shared/ui/Sparkline';
import { vitalsHistoryLength } from '../model';

interface VitalChartProps {
  label: string;
  // value is the newest reading, written out beside the label.
  value: string;
  sub?: string;
  // series is what the page has sampled since it opened, oldest first.
  series: readonly number[];
  max: number;
}

// VitalChart is one live reading over the last 90 seconds: its label and
// newest value (the chart's direct label), then a line of the samples taken
// while the page is open, laid from the right so the newest is always at
// "now". It starts empty and draws a line once two samples exist.
export function VitalChart({ label, value, sub, series, max }: VitalChartProps) {
  const t = useT();
  return (
    <section className="min-w-0">
      <div className="flex items-baseline justify-between gap-3">
        <h3 className="truncate text-[12px] font-semibold text-label-2">{label}</h3>
        <span className="shrink-0 text-right">
          <span className="text-[20px] font-[650] tabular-nums tracking-[-0.02em] text-label">{value}</span>
          {sub && <span className="ml-1.5 text-[12px] text-label-2">{sub}</span>}
        </span>
      </div>
      <div role="img" aria-label={t('admin.monitoring.chartLabel', { label })}>
        <Sparkline series={series} max={max} slots={vitalsHistoryLength} guides className="mt-2 h-24" />
      </div>
      <div aria-hidden className="mt-1 flex justify-between text-[11px] text-label-3">
        <span>{t('admin.monitoring.chartAgo')}</span>
        <span>{t('admin.monitoring.chartNow')}</span>
      </div>
    </section>
  );
}
