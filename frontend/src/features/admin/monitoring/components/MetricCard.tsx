import type { LucideIcon } from 'lucide-react';

import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/Card';
import { formatBytes } from '@/shared/lib/utils';

function progressColor(percent: number) {
  if (percent < 70) return 'bg-accent-fill';
  if (percent < 90) return 'bg-orange';
  return 'bg-red';
}

/** A host metric with a usage bar. */
export function MetricCard({
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
          <Icon className="w-5 h-5 text-label-2" />
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
