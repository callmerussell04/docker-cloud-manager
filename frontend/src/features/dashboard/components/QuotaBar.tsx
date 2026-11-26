import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { quotaLevel, type QuotaLevel } from '../model';

const levelFill: Record<QuotaLevel, string> = {
  ok: 'bg-accent-fill',
  high: 'bg-orange',
  full: 'bg-red',
};

interface QuotaBarProps {
  label: string;
  used: number;
  total: number;
  // format writes an amount, such as bytes; plain numbers by default.
  format?: (value: number) => string;
}

// QuotaBar is one figure against its quota: the label, the amount of the
// quota and the share used, over a bar that turns orange from 75% and red
// from 90%. The share is written out, so the colour never stands alone.
export function QuotaBar({ label, used, total, format = String }: QuotaBarProps) {
  const t = useT();
  const level = quotaLevel(used, total);
  const share = total > 0 ? Math.min(used / total, 1) : 0;
  const percent = total > 0 ? Math.round((used / total) * 100) : 0;
  return (
    <div className="grid gap-2">
      <div className="flex items-baseline justify-between gap-3">
        <span className="min-w-0 truncate font-semibold text-label">{label}</span>
        <span className="shrink-0 whitespace-nowrap tabular-nums text-label-2">
          {t('dashboard.quota.of', { used: format(used), total: format(total) })}
          <span className={cn('ml-2', level === 'full' ? 'font-semibold text-red-text' : level === 'high' ? 'font-semibold text-label' : '')}>{percent}%</span>
        </span>
      </div>
      <div aria-hidden className="h-1.5 overflow-hidden rounded-full bg-fill">
        <span className={cn('block h-full rounded-full', levelFill[level])} style={{ width: `${share * 100}%` }} />
      </div>
    </div>
  );
}
