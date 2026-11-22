import type { LucideIcon } from 'lucide-react';

import { Card, CardContent } from '@/shared/ui/Card';

export function ReportMetricCard({
  title,
  value,
  icon: Icon,
}: {
  title: string;
  value: string;
  icon: LucideIcon;
}) {
  return (
    <Card>
      <CardContent className="flex items-center gap-4 p-5">
        <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-lg bg-fill text-label-2">
          <Icon className="h-5 w-5" />
        </div>
        <div className="min-w-0">
          <p className="truncate text-sm font-medium text-label-2">{title}</p>
          <p className="truncate text-2xl font-semibold text-label">{value}</p>
        </div>
      </CardContent>
    </Card>
  );
}
