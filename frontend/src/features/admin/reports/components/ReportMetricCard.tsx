import type { LucideIcon } from 'lucide-react';

import { Card, CardContent } from '@/shared/ui/Card';

export function ReportMetricCard({
  title,
  value,
  icon: Icon,
  tone = 'sky',
}: {
  title: string;
  value: string;
  icon: LucideIcon;
  tone?: 'sky' | 'indigo' | 'emerald' | 'amber' | 'slate';
}) {
  const toneClass = {
    sky: 'bg-sky-50 text-sky-700 dark:bg-sky-950/40 dark:text-sky-300',
    indigo: 'bg-indigo-50 text-indigo-700 dark:bg-indigo-950/40 dark:text-indigo-300',
    emerald: 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300',
    amber: 'bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-300',
    slate: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300',
  }[tone];

  return (
    <Card>
      <CardContent className="flex items-center gap-4 p-5">
        <div className={`flex h-11 w-11 shrink-0 items-center justify-center rounded-lg ${toneClass}`}>
          <Icon className="h-5 w-5" />
        </div>
        <div className="min-w-0">
          <p className="truncate text-sm font-medium text-slate-500 dark:text-slate-400">{title}</p>
          <p className="truncate text-2xl font-semibold text-slate-900 dark:text-slate-100">{value}</p>
        </div>
      </CardContent>
    </Card>
  );
}
