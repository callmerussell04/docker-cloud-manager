import type { LucideIcon } from 'lucide-react';

import { Card, CardContent } from '@/shared/ui/Card';

export function CountCard({ label, value, icon: Icon }: { label: string; value: number; icon: LucideIcon }) {
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
